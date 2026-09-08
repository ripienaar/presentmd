package present

import (
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Errors returned by Export.
var (
	// ErrDeckProblems is a deck that loaded or rendered with problems. The export
	// refuses it and writes nothing: a slide whose page style the theme does not
	// have is missing from the page, and a talk with a slide missing is not one
	// to hand to anyone. What the inlining meets is reported and the file is
	// still written.
	ErrDeckProblems = errors.New("the deck has problems")
	// ErrExportRead is reveal's vendored files or the theme's stylesheet failing
	// to read. The binary carries the first and a loaded theme has already read
	// the second, so either is a broken install rather than a deck's problem.
	ErrExportRead = errors.New("cannot read a file the page is built from")
	// ErrExportPage is a page that does not name a reference the exporter came to
	// replace, which is the page template and this file disagreeing rather than
	// anything a deck did.
	ErrExportPage = errors.New("cannot inline the deck page")
)

const (
	// avatarHost is where a presenter's github handle is fetched from. This is
	// the only place planboard reaches the network: a served deck points the
	// browser at the URL, and the loader runs on every board request and never
	// fetches at all.
	avatarHost = "https://github.com/"
	// avatarTimeout limits the one fetch an export makes, so a handle whose
	// avatar does not arrive costs the export ten seconds rather than the file.
	avatarTimeout = 10 * time.Second
	// maxAvatarBytes is the most of an avatar response that is read into the
	// page.
	maxAvatarBytes = 8 << 20
	// avatarType is what an avatar is carried as when the response does not say.
	avatarType = "image/png"
)

var (
	// scriptBreakRe is what stops a script being written into the page as its own
	// text. A closing tag ends the element early, and an HTML comment opener
	// ahead of an opening script tag puts the parser in a state where the closing
	// tag the exporter wrote does not end it at all. Reveal's notes plugin holds
	// both inside the regexps of the markdown parser it ships with, so the plugin
	// becomes a data URI. Reveal's own script holds neither and is written out.
	scriptBreakRe = regexp.MustCompile(`(?i)</script|<script[\s/>]|<!--`)

	// styleBreakRe is the same for a stylesheet. A directory theme's CSS is
	// written outside this repository, so it is checked rather than trusted.
	styleBreakRe = regexp.MustCompile(`(?i)</style`)
)

// exportOptions are the parts of an export a test replaces. Export takes none:
// where an avatar is fetched from is not the caller's to choose.
type exportOptions struct {
	client *http.Client
	// host stands in for avatarHost, so a test fetches from its own server
	// rather than from github.com.
	host string
}

// Export renders deck through theme into one HTML file that opens from file://
// with nothing left to fetch. Reveal's script and stylesheets, the theme's
// stylesheet and the files it names, the avatar and every image the slides and
// backgrounds name are carried in the page itself. What a slide's own prose
// links to on the web is left as the author wrote it.
//
// The problems of the load and of the render are the deck's own and are fatal:
// they come back with no page and ErrDeckProblems. The problems the inlining
// meets, such as an avatar that will not fetch and a background video that has
// no sensible data URI, come back with the page, which still names the file it
// could not carry.
func Export(deck *Deck, theme *Theme) ([]byte, []Problem, error) {
	return export(deck, theme, exportOptions{})
}

func export(deck *Deck, theme *Theme, opts exportOptions) ([]byte, []Problem, error) {
	rendered, err := RenderDeck(deck, theme, RenderOptions{})
	if err != nil {
		return nil, nil, err
	}

	fatal := make([]Problem, 0, len(deck.Problems)+len(rendered.Problems))
	fatal = append(fatal, deck.Problems...)
	fatal = append(fatal, rendered.Problems...)

	if len(fatal) > 0 {
		return nil, fatal, ErrDeckProblems
	}

	var problems []Problem

	presenter := deck.Presentation.Presenter

	// A handle with no avatar file beside it is the one case that fetches. The
	// deck rendered without problems above, so the handle is one GitHub could
	// issue, and the page is rendered again with whatever came back rather than
	// with the URL.
	if presenter.Avatar == "" && presenter.GitHub != "" {
		avatar, problem := fetchAvatar(deck.presentationPath(), presenter.GitHub, opts)
		if problem != nil {
			problems = append(problems, *problem)
		}

		rendered, err = RenderDeck(deck, theme, RenderOptions{Avatar: &avatar})
		if err != nil {
			return nil, problems, err
		}
	}

	page, inlined, err := inlinePage(rendered, deck, theme)
	problems = append(problems, inlined...)

	if err != nil {
		return nil, problems, err
	}

	return []byte(page), problems, nil
}

// fetchAvatar reads the avatar a github handle stands in for. A fetch that does
// not answer, or answers with anything but the image, is a problem and an empty
// avatar: the slot is left empty rather than pointing a file that is meant to
// open offline at github.com.
func fetchAvatar(at string, handle string, opts exportOptions) (string, *Problem) {
	client := opts.client
	if client == nil {
		client = &http.Client{Timeout: avatarTimeout}
	}

	host := opts.host
	if host == "" {
		host = avatarHost
	}

	url := host + handle + ".png"

	resp, err := client.Get(url)
	if err != nil {
		return "", avatarProblem(at, url, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", avatarProblem(at, url, "answered "+resp.Status)
	}

	// One byte past the limit is read so a response that runs on is refused
	// rather than carried into the page as an image cut in half.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return "", avatarProblem(at, url, err.Error())
	}

	if len(data) > maxAvatarBytes {
		return "", avatarProblem(at, url, fmt.Sprintf("answered with more than %d bytes", maxAvatarBytes))
	}

	return dataURIOf(avatarContentType(resp), data), nil
}

func avatarProblem(at string, url string, message string) *Problem {
	return &Problem{
		Path:    at,
		Message: fmt.Sprintf("cannot fetch the avatar at %s, so the slot is empty: %s", url, message),
	}
}

// avatarContentType is what the response says the avatar is, and png where it
// says something that is not an image. GitHub answers a .png request with a
// jpeg for a handle whose avatar was uploaded as one.
func avatarContentType(resp *http.Response) string {
	ctype, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")

	ctype = strings.TrimSpace(ctype)
	if !strings.HasPrefix(ctype, "image/") {
		return avatarType
	}

	return ctype
}

// inlinePage rewrites every reference the rendered page makes into the page
// itself. The deck's own files go first: the reveal and theme content written in
// after them holds data URIs of its own, and walking those for references costs
// time and finds nothing.
func inlinePage(rendered *RenderedDeck, deck *Deck, theme *Theme) (string, []Problem, error) {
	page := rendered.HTML

	var problems []Problem

	// A deck built in memory rather than by LoadDeck holds no root, and names no
	// files it could serve either.
	if deck.Root() != nil && len(rendered.Assets) > 0 {
		uris, deckProblems := inlineFiles(deck.Root().FS(), rendered.Assets, "")
		problems = append(problems, deckProblems...)

		page = rewriteRefs(page, func(ref string) (string, bool) {
			asset, ok := deckRef(ref)
			if !ok {
				return "", false
			}

			uri, held := uris[asset]

			return uri, held
		})
	}

	page, err := inlineReveal(page)
	if err != nil {
		return "", problems, err
	}

	css, themeProblems, err := inlineThemeCSS(theme, rendered.ThemeAssets)
	problems = append(problems, themeProblems...)

	if err != nil {
		return "", problems, err
	}

	themeRef := RenderOptions{}.assetRef(themePrefix, themeStylesheet)

	page, err = replaceElement(page, linkElement(themeRef), inlineStyle(css, themeStylesheet))
	if err != nil {
		return "", problems, err
	}

	return page, problems, nil
}

// revealFile is one of reveal's vendored files as the page names it.
type revealFile struct {
	name string
	css  bool
}

// inlineReveal writes reveal's stylesheets and scripts into the page in place of
// the links and script tags that point beside it.
func inlineReveal(page string) (string, error) {
	revealFS := RevealFS()

	files := []revealFile{
		{name: revealResetCSS, css: true},
		{name: revealCoreCSS, css: true},
		{name: revealScript},
		{name: revealNotesPlug},
	}

	for _, file := range files {
		data, err := fs.ReadFile(revealFS, file.name)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrExportRead, err)
		}

		ref := RenderOptions{}.assetRef(revealPrefix, file.name)

		if file.css {
			page, err = replaceElement(page, linkElement(ref), inlineStyle(data, file.name))
		} else {
			page, err = replaceElement(page, scriptSrcElement(ref), inlineScript(data, file.name))
		}

		if err != nil {
			return "", err
		}
	}

	return page, nil
}

// inlineThemeCSS is the theme's stylesheet with every url() target it names
// carried in the stylesheet itself. The stylesheet reaches the file as part of
// the page rather than as a file beside it, so a target left as a relative path
// would be resolved against wherever the file was saved.
func inlineThemeCSS(theme *Theme, assets []string) ([]byte, []Problem, error) {
	css, err := fs.ReadFile(theme.FS(), themeStylesheet)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrExportRead, err)
	}

	uris, problems := inlineFiles(theme.FS(), assets, themeStylesheet)

	// An @import names a stylesheet this does not follow, so it would be left as
	// a relative reference in a file whose whole promise is that it opens with no
	// network. It is reported rather than followed: a theme that imports is rare,
	// and saying so is what keeps the failure from being a silent one.
	for _, match := range cssImportRe.FindAllString(string(css), -1) {
		problems = append(problems, Problem{
			Path:    themeStylesheet,
			Message: fmt.Sprintf("the theme stylesheet holds %q, which the export does not follow, so the file is not self contained", strings.TrimSpace(match)),
		})
	}

	rewritten := rewriteCSSRefs(string(css), func(ref string) (string, bool) {
		asset, ok := relativeAsset(ref)
		if !ok {
			return "", false
		}

		uri, held := uris[asset]

		return uri, held
	})

	return []byte(rewritten), problems, nil
}

// inlineFiles reads every file of assets out of fsys and answers with the data
// URI each becomes. A file that will not read, and one whose type has no
// sensible data URI, is a problem and is left out, so the page still names the
// path and says what it lacks. owner is the file the references were written
// in, and is empty for the deck's own files, which are reported against
// themselves.
func inlineFiles(fsys fs.FS, assets []string, owner string) (map[string]string, []Problem) {
	uris := make(map[string]string, len(assets))

	var problems []Problem

	for _, asset := range assets {
		at := owner
		if at == "" {
			at = asset
		}

		ctype := contentType(asset)
		if !inlinable(ctype) {
			problems = append(problems, Problem{
				Path:    at,
				Message: fmt.Sprintf("%s is %s, which has no sensible data uri, so the file still points at the path", asset, ctype),
			})

			continue
		}

		data, err := fs.ReadFile(fsys, asset)
		if err != nil {
			problems = append(problems, Problem{
				Path:    at,
				Message: fmt.Sprintf("cannot read %s to carry it in the file: %s", asset, err),
			})

			continue
		}

		uris[asset] = dataURIOf(ctype, data)
	}

	return uris, problems
}

// inlinable says whether a type belongs in a data URI. A video is the case the
// design names: a background video is tens of megabytes and base64 makes a file
// no browser opens, so it is reported and left as its path. A type the table
// does not know is refused for the same reason a browser would drop it.
func inlinable(ctype string) bool {
	return !strings.HasPrefix(ctype, "video/") && ctype != "application/octet-stream"
}

// deckRef turns a reference the page makes under deckAssetDir back into the deck
// file it names, spelled as the asset list spells it. Everything else, a link a
// slide wrote to the web included, is not the deck's to carry.
func deckRef(ref string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(html.UnescapeString(ref)), deckAssetDir+"/")
	if !ok {
		return "", false
	}

	return relativeAsset(rest)
}

// dataURIOf is one file as the page carries it, always base64: the encoding
// costs a third of the size and takes fonts, images and stylesheets alike
// without a table of what has to be escaped.
func dataURIOf(ctype string, data []byte) string {
	// A data URI takes no space in its media type, and the table answers css and
	// the text types with a charset parameter.
	ctype = strings.ReplaceAll(ctype, " ", "")

	return "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// linkElement and scriptSrcElement are how the page points at a file beside it,
// written exactly as the page template writes them, since those are the
// elements the inlined content replaces.
func linkElement(ref string) string {
	return `<link rel="stylesheet" href="` + ref + `">`
}

func scriptSrcElement(ref string) string {
	return `<script src="` + ref + `"></script>`
}

// inlineStyle is a stylesheet as the page carries it, written out where its text
// cannot end the element it is written into and carried as a data URI where it
// can.
func inlineStyle(css []byte, name string) string {
	if styleBreakRe.Match(css) {
		return linkElement(dataURIOf(contentType(name), css))
	}

	return "<style>\n" + string(css) + "\n</style>"
}

func inlineScript(js []byte, name string) string {
	if scriptBreakRe.Match(js) {
		return scriptSrcElement(dataURIOf(contentType(name), js))
	}

	return "<script>\n" + string(js) + "\n</script>"
}

// replaceElement swaps the one element the page writes for a reference with the
// content itself. A page that does not hold it is the page template and this
// file disagreeing about how a reference is written, which is a bug in the
// binary rather than anything a deck did, so it stops the export.
func replaceElement(page string, element string, inlined string) (string, error) {
	if !strings.Contains(page, element) {
		return "", fmt.Errorf("%w: the page does not hold %s", ErrExportPage, element)
	}

	return strings.Replace(page, element, inlined, 1), nil
}
