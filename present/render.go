package present

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"html"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
	"text/template"
)

// Errors returned by RenderDeck. Everything a single slide is wrong about is a
// Problem against that slide instead, since a deck of thirty slides with one
// bad page style still has twenty nine to show.
var (
	ErrNoDeck         = errors.New("no deck to render")
	ErrNoTheme        = errors.New("no theme to render with")
	ErrRenderMarkdown = errors.New("cannot render slide markdown")
	ErrCodeStylesheet = errors.New("cannot write the code stylesheet")
	ErrRenderPage     = errors.New("cannot render the deck page")

	// ErrSecondBreak is internal to the render: a split page style divides at one
	// break and the caller turns this into a problem against the slide.
	ErrSecondBreak = errors.New("a split page style holds a second thematic break")
)

const (
	// revealPrefix and themePrefix are where the page expects reveal's vendored
	// files and the theme's stylesheet to be served, relative to the page itself.
	// Everything the page names is relative so one rendering serves at / under
	// present serve and under /present/<project>/<deck>/ on the board, and so the
	// exporter can resolve each reference against the deck directory.
	revealPrefix = "reveal"
	themePrefix  = "theme"

	revealResetCSS  = "reset.css"
	revealCoreCSS   = "reveal.css"
	revealScript    = "reveal.js"
	revealNotesPlug = "plugin/notes.js"

	// defaultTransition is what a deck whose presentation and theme both name
	// nothing moves with, an instant move, so an animation is opted into rather
	// than out of.
	defaultTransition = "none"
	// defaultTransitionSpeed is reveal's own, which applies to whatever
	// transition a deck asks for.
	defaultTransitionSpeed = "default"

	// defaultTextSize is the size a slide that names nothing is set at, and it
	// reaches the page as no attribute at all so a theme's own sizing applies
	// with nothing overriding it.
	defaultTextSize = "normal"

	// digestLength is how much of a sha256 the page carries. The digests are
	// compared between two renders by the same renderer rather than trusted
	// against anything, and sixteen characters is short enough to read in a page
	// and long enough that two slides of one deck do not collide.
	digestLength = 16

	// shellMeta is where the page writes the digest of everything a slide swap in
	// the browser cannot update. The live page compares its own against the one
	// on the page it fetched and reloads when they differ.
	shellMeta = "planboard-shell"
)

// transitions and transitionSpeeds are reveal's own value sets. Reveal drops a
// word outside them without saying so, so a deck naming one is told here
// instead of finding out from a talk that does not move the way it was written
// to.
var (
	transitions      = []string{"none", "fade", "slide", "convex", "concave", "zoom"}
	transitionSpeeds = []string{"default", "fast", "slow"}
)

// textSizes are the steps a slide's body and code are set at. They are named
// rather than numbered because the theme decides what a step means: a deck asks
// for smaller and the theme keeps the set of slides looking related, where a
// number would let one slide sit at any size its author typed.
var textSizes = []string{"small", defaultTextSize, "large", "huge"}

// gitHubHandleRe is GitHub's handle grammar: letters, digits and hyphens, with
// no hyphen at either end. The length is checked beside it. A handle is turned
// into an avatar URL rather than fetched, so it is checked here rather than
// found to be wrong by a browser that renders a broken image on the title
// slide.
var gitHubHandleRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

// maxGitHubHandle is the longest handle GitHub issues.
const maxGitHubHandle = 39

// RenderOptions are the parts of a page that depend on who is serving it rather
// than on the deck.
type RenderOptions struct {
	// EventsURL is where the page listens for the reload event that follows an
	// edit. It is empty for a rendering nobody is watching, such as an export or
	// a test, and the page then carries no reload script at all.
	EventsURL string
	// AssetPrefix is put in front of the reveal and theme references, for a
	// server that serves those somewhere other than beside the page. It has to
	// stay relative: an absolute one is what breaks a deck served under a path
	// prefix, which is the case the relative references exist for.
	AssetPrefix string
	// Avatar replaces the avatar every page style template is given. The
	// exporter sets it to the data URI it inlined the avatar as, and to an empty
	// string when the one fetch it makes for a github handle failed, which leaves
	// the slot empty rather than pointing a file that opens offline at
	// github.com. A nil pointer leaves the avatar as the presentation named it.
	Avatar *string
}

// RenderedDeck is a deck as a page: the HTML, the files that page names, and
// what went wrong rendering it.
type RenderedDeck struct {
	HTML string
	// Assets are the paths the page names, relative to the deck directory, each
	// one a file the server has to answer for and the exporter has to inline
	// through the deck's root.
	Assets []string
	// ThemeAssets are the url() targets of the theme's stylesheet, relative to
	// the theme. They are a list of their own because they are read through the
	// theme's file system rather than the deck's, and one list of both left the
	// exporter holding a path with nothing to say which to open it through.
	ThemeAssets []string
	// Problems are the render's own, which do not include the ones LoadDeck
	// already holds against the deck. A slide with a problem here is left out of
	// the page rather than replacing the slides around it, so a deck renders as
	// far as it can and the caller decides what a problem means.
	Problems []Problem
}

// RenderDeck renders a loaded deck through its theme into one reveal page.
//
// The two arguments are separate because a theme outlives a deck: the board
// loads a deck on every request and a directory theme is read once, and the
// render is the only place the two meet.
func RenderDeck(deck *Deck, theme *Theme, opts RenderOptions) (*RenderedDeck, error) {
	if deck == nil {
		return nil, ErrNoDeck
	}

	if theme == nil {
		return nil, ErrNoTheme
	}

	md := newSlideMarkdown(theme.Config.CodeStyle)
	out := &RenderedDeck{}

	show := &deck.Presentation
	data := NewTemplateData(show)

	problem := checkAvatar(deck.presentationPath(), show)
	if problem != nil {
		data.Avatar = ""

		out.Problems = append(out.Problems, *problem)
	}

	if opts.Avatar != nil {
		data.Avatar = *opts.Avatar
	}

	move, problems := resolveTransition(deck.presentationPath(), show, theme)
	out.Problems = append(out.Problems, problems...)

	var sections strings.Builder

	for _, slide := range deck.Slides {
		transition, problem := slideTransition(slide)
		if problem != nil {
			out.Problems = append(out.Problems, *problem)
		}

		textSize, problem := slideTextSize(slide)
		if problem != nil {
			out.Problems = append(out.Problems, *problem)
		}

		// A slide reports what is wrong with it and still renders where it can: a
		// page style the theme does not have leaves nothing to place and the slide
		// is dropped, where a tint nobody defined is one word the wrong colour.
		section, problems := renderSlide(md, theme, data, slide, transition, textSize, opts.EventsURL != "")

		out.Problems = append(out.Problems, problems...)

		if section == "" {
			continue
		}

		sections.WriteString(section)
	}

	stylesheet, err := md.stylesheet()
	if err != nil {
		return nil, err
	}

	// The lists are read off the sections as the slides wrote them, since they
	// hold file names. The page then writes those references under the deck's own
	// path segment, which is what keeps a deck directory called reveal or theme
	// from having its files answered by the vendored reveal or by the theme.
	out.Assets, out.ThemeAssets = collectAssets(sections.String(), theme.FS())

	// A page nobody is watching carries no digest, which is what keeps an export
	// the same bytes it was before the live page learned to swap a slide.
	files := ""
	if opts.EventsURL != "" {
		files = fileDigest(deck, theme, out.Assets, out.ThemeAssets)
	}

	page, err := renderPage(show, theme, prefixAssets(sections.String()), stylesheet, move, files, opts)
	if err != nil {
		return nil, err
	}

	out.HTML = page

	return out, nil
}

// fileDigest digests the files the page names but does not carry: the theme's
// stylesheet by its contents, and every asset the render listed by its size and
// the time it was last written.
//
// The assets are in it because editing a slide's image re-renders a page that is
// byte identical to the one being shown. Nothing in the HTML changed, so the
// live page has nothing to swap, and the deck's files are served with no
// validator, so a reload is what fetches the new image.
func fileDigest(deck *Deck, theme *Theme, assets []string, themeAssets []string) string {
	sum := sha256.New()

	css, err := fs.ReadFile(theme.FS(), themeStylesheet)
	if err != nil {
		fmt.Fprintf(sum, "%s error %s\n", themeStylesheet, err)
	} else {
		sum.Write(css)
	}

	// A deck built in memory rather than read from a directory holds no root, and
	// its assets are digested by name alone.
	var files fs.FS

	if deck.Root() != nil {
		files = deck.Root().FS()
	}

	digestFiles(sum, files, assets)
	digestFiles(sum, theme.FS(), themeAssets)

	return hex.EncodeToString(sum.Sum(nil))[:digestLength]
}

// digestFiles adds each named file to sum. A file that cannot be read is added
// as the error it failed with, which changes the digest and reloads the page
// rather than leaving a swap to update something it cannot see.
func digestFiles(sum hash.Hash, fsys fs.FS, names []string) {
	for _, name := range names {
		if fsys == nil {
			fmt.Fprintf(sum, "%s\n", name)

			continue
		}

		info, err := fs.Stat(fsys, name)
		if err != nil {
			fmt.Fprintf(sum, "%s error %s\n", name, err)

			continue
		}

		fmt.Fprintf(sum, "%s %d %d\n", name, info.Size(), info.ModTime().UnixNano())
	}
}

// shellDigest is what the meta carries: the file digest together with the values
// reveal was initialized with and the stylesheets the page loaded. A change to
// any of them is a reload, since none of them can be put in place under a reveal
// instance that is already running.
func shellDigest(data pageData, files string) string {
	sum := sha256.New()

	fmt.Fprintf(sum, "files %s\n", files)
	fmt.Fprintf(sum, "css %s %s %s\n", data.ResetCSS, data.RevealCSS, data.ThemeCSS)
	fmt.Fprintf(sum, "js %s %s\n", data.RevealJS, data.NotesJS)

	for _, font := range data.Fonts {
		fmt.Fprintf(sum, "font %s %s\n", font.Name, font.Stack)
	}

	fmt.Fprintf(sum, "chroma %s\n", data.Chroma)
	fmt.Fprintf(sum, "reveal %d %d %s %s\n", data.Width, data.Height, data.Transition, data.TransitionSpeed)

	return hex.EncodeToString(sum.Sum(nil))[:digestLength]
}

// shortDigest is the digest a section carries, one for its attributes and one
// for what is inside it.
func shortDigest(text string) string {
	sum := sha256.Sum256([]byte(text))

	return hex.EncodeToString(sum[:])[:digestLength]
}

// checkAvatar reports a presenter whose github handle is not one. The avatar URL
// is built from the handle without asking GitHub, so a handle that cannot be one
// would otherwise reach the page as a URL that answers with a 404 image.
func checkAvatar(at string, show *Presentation) *Problem {
	if show.Presenter.Avatar != "" || show.Presenter.GitHub == "" {
		return nil
	}

	handle := show.Presenter.GitHub
	if len(handle) <= maxGitHubHandle && gitHubHandleRe.MatchString(handle) {
		return nil
	}

	return &Problem{
		Path:    at,
		Message: fmt.Sprintf("presenter github %q is not a github handle, so no avatar was placed", handle),
	}
}

// deckTransition is the move reveal makes between slides and how fast it runs,
// resolved from the presentation, the theme and the defaults into the pair the
// page hands reveal.
type deckTransition struct {
	name  string
	speed string
}

// resolveTransition settles that pair. The deck's transition wins over the
// theme's, what neither names is none, and a speed is the deck's alone. A value
// outside reveal's sets counts as unnamed, so it is a problem against the file
// that wrote it and the deck moves the way it would have without the line.
func resolveTransition(at string, show *Presentation, theme *Theme) (deckTransition, []Problem) {
	move := deckTransition{name: defaultTransition, speed: defaultTransitionSpeed}

	var problems []Problem

	themed, problem := checkChoice(themeConfigPath(theme), "transition", theme.Config.Transition, transitions)
	if problem != nil {
		problems = append(problems, *problem)
	}

	if themed != "" {
		move.name = themed
	}

	named, problem := checkChoice(at, "transition", show.Transition, transitions)
	if problem != nil {
		problems = append(problems, *problem)
	}

	if named != "" {
		move.name = named
	}

	speed, problem := checkChoice(at, "transition_speed", show.TransitionSpeed, transitionSpeeds)
	if problem != nil {
		problems = append(problems, *problem)
	}

	if speed != "" {
		move.speed = speed
	}

	return move, problems
}

// slideTransition is reveal's per-slide override, and is empty for a slide that
// names none and for one whose value reveal does not know. The slide still
// renders in either case: the deck's own transition is what a slide with no
// usable override moves with.
func slideTransition(slide *Slide) (string, *Problem) {
	return checkChoice(slide.Path, "transition", slide.Transition, transitions)
}

// slideTextSize is the step the theme sets a slide's body and its code at. It
// comes back empty for a slide at the normal size and for one naming a step the
// themes do not carry, and the slide renders at the normal size in both cases: a
// size nobody can spell is no reason to drop a slide from a talk.
func slideTextSize(slide *Slide) (string, *Problem) {
	size, problem := checkChoice(slide.Path, "text_size", slide.TextSize, textSizes)
	if size == defaultTextSize {
		return "", nil
	}

	return size, problem
}

// checkChoice answers with the value where it is one of the set the field takes,
// and with a problem against the file that wrote it where it is not. An empty
// value comes back empty and unreported, since naming nothing is how a deck, a
// theme and a slide all leave the decision to whoever else named one.
func checkChoice(at string, field string, value string, allowed []string) (string, *Problem) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	if slices.Contains(allowed, value) {
		return value, nil
	}

	return "", &Problem{
		Path:    at,
		Message: fmt.Sprintf("%s %q is not one of %s, so it was left out", field, value, strings.Join(allowed, ", ")),
	}
}

// themeConfigPath names the theme's own theme.yaml for a problem written
// against it. The theme is not a file under the deck, so the theme's name is
// what says which theme.yaml carries the line.
func themeConfigPath(theme *Theme) string {
	return path.Join(theme.Name, themeConfigFile)
}

// renderSlide renders one slide into the <section> it occupies. The section
// element is written here rather than by a template, so the page style class,
// the reveal state and the background, transition and text size attributes are
// on every slide whatever a theme's author remembered.
//
// A live page's sections carry a digest of their attributes and a digest of what
// is inside them. The page compares those against the ones on the render it
// fetched: a section whose attributes match and whose body does not is a slide
// whose text changed, and only that slide's contents are put in place. Digests
// rather than the attributes themselves, because reveal writes its own onto
// every section as it runs and the page would have to know which of them are
// reveal's.
func renderSlide(md *slideMarkdown, theme *Theme, data TemplateData, slide *Slide, transition string, textSize string, live bool) (string, []Problem) {
	if !theme.HasStyle(slide.PageStyle) {
		return "", []Problem{{
			Path:    slide.Path,
			Message: fmt.Sprintf("theme %s has no page style %q, it has %s", theme.Name, slide.PageStyle, strings.Join(theme.Styles(), ", ")),
		}}
	}

	// Every slide's ids land in the one page reveal serves, so each slide's carry
	// its number in front of them.
	md.slidePrefix = fmt.Sprintf("s%d-", slide.Number)
	md.resetTints()

	body, err := md.render(slide.Markdown, theme.Splits(slide.PageStyle))
	if errors.Is(err, ErrSecondBreak) {
		return "", []Problem{{
			Path:    slide.Path,
			Message: fmt.Sprintf("page style %q divides at its first thematic break and the slide holds a second", slide.PageStyle),
		}}
	}

	if err != nil {
		return "", []Problem{{Path: slide.Path, Message: err.Error()}}
	}

	caption, err := md.renderInline(slide.Caption)
	if err != nil {
		return "", []Problem{{Path: slide.Path, Message: fmt.Sprintf("caption: %s", err)}}
	}

	cta, err := md.renderInline(slide.CTA)
	if err != nil {
		return "", []Problem{{Path: slide.Path, Message: fmt.Sprintf("cta: %s", err)}}
	}

	notes, err := md.renderMarkdown(slide.Notes)
	if err != nil {
		return "", []Problem{{Path: slide.Path, Message: fmt.Sprintf("notes: %s", err)}}
	}

	data.Slide = slide
	data.Heading = body.Heading
	data.Body = body.Body
	data.Left = body.Left
	data.Right = body.Right
	data.Caption = caption
	data.CTA = cta

	inside, err := theme.Render(slide.PageStyle, data)
	if err != nil {
		return "", []Problem{{Path: slide.Path, Message: err.Error()}}
	}

	style := html.EscapeString(slide.PageStyle)

	attributes := fmt.Sprintf(`class="slide-%s" data-state="%s"%s%s%s`, style, style, backgroundAttribute(slide.Background), transitionAttribute(transition), textSizeAttribute(textSize))

	var inner strings.Builder

	inner.WriteString(strings.TrimSpace(inside))
	inner.WriteString("\n")

	if notes != "" {
		inner.WriteString(`<aside class="notes">`)
		inner.WriteString("\n")
		inner.WriteString(strings.TrimSpace(notes))
		inner.WriteString("\n</aside>\n")
	}

	var section strings.Builder

	section.WriteString("<section ")
	section.WriteString(attributes)

	if live {
		fmt.Fprintf(&section, ` data-slide-shell="%s" data-slide-body="%s"`, shortDigest(attributes), shortDigest(inner.String()))
	}

	section.WriteString(">\n")
	section.WriteString(inner.String())
	section.WriteString("</section>\n")

	// The slide still renders: a tint nobody defined is a word that came out the
	// colour of the prose around it, which is worth a line against the file rather
	// than a slide the deck loses.
	var problems []Problem

	for _, tint := range md.tintProblems() {
		problems = append(problems, Problem{Path: slide.Path, Message: tint})
	}

	return section.String(), problems
}

// transitionAttribute is a slide's own transition as reveal reads it, and is
// empty for a slide that moves the way the rest of the deck does.
func transitionAttribute(transition string) string {
	if transition == "" {
		return ""
	}

	return fmt.Sprintf(` data-transition="%s"`, html.EscapeString(transition))
}

// textSizeAttribute is the step the theme reads a slide's body and code size
// from, and is empty for a slide at the normal size, which leaves the theme's
// own sizing with nothing overriding it.
func textSizeAttribute(size string) string {
	if size == "" {
		return ""
	}

	return fmt.Sprintf(` data-text-size="%s"`, html.EscapeString(size))
}

// backgroundAttribute turns a slide's background into the reveal attribute it
// means. A value opening with # and a CSS color name are the two ways a person
// writes a color, and everything else is a path to an image, which is the form
// that also has to reach the asset list.
func backgroundAttribute(background string) string {
	background = strings.TrimSpace(background)
	if background == "" {
		return ""
	}

	if strings.HasPrefix(background, "#") || isColorName(background) {
		return fmt.Sprintf(` data-background-color="%s"`, html.EscapeString(background))
	}

	return fmt.Sprintf(` data-background-image="%s"`, html.EscapeString(background))
}

// cssColorNames are the CSS named colors, so a slide that says background:
// navy gets the color it asked for rather than a request for a file named navy.
// The list is the whole of the CSS Color level 4 set: a shorter one would send
// papayawhip to the image branch, and the failure a person sees for that is a
// slide with no background and no message.
const cssColorNames = `aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue
blueviolet brown burlywood cadetblue chartreuse chocolate coral cornflowerblue cornsilk crimson cyan
darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta darkolivegreen
darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey
darkturquoise darkviolet deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite
forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green greenyellow grey honeydew hotpink
indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral
lightcyan lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen
lightskyblue lightslategray lightslategrey lightsteelblue lightyellow lime limegreen linen magenta
maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen mediumslateblue
mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin
navajowhite navy oldlace olive olivedrab orange orangered orchid palegoldenrod palegreen
paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple rebeccapurple red
rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue
slategray slategrey snow springgreen steelblue tan teal thistle tomato turquoise violet wheat white
whitesmoke yellow yellowgreen transparent currentcolor`

var colorNames = colorNameSet()

func colorNameSet() map[string]struct{} {
	names := strings.Fields(cssColorNames)
	set := make(map[string]struct{}, len(names))

	for _, name := range names {
		set[name] = struct{}{}
	}

	return set
}

func isColorName(value string) bool {
	_, ok := colorNames[strings.ToLower(value)]

	return ok
}

// fontProperty is one of the three font roles as the custom property the
// theme's stylesheet reads it from.
type fontProperty struct {
	Name  string
	Stack string
}

// pageData is what the page template is executed with. Every field is already
// the text it is written as: the template is text/template rather than
// html/template because the sections and chroma's stylesheet are finished HTML
// and CSS, and contextual escaping would rewrite both.
type pageData struct {
	Title           string
	ResetCSS        string
	RevealCSS       string
	ThemeCSS        string
	RevealJS        string
	NotesJS         string
	Fonts           []fontProperty
	Chroma          string
	Sections        string
	Width           int
	Height          int
	Transition      string
	TransitionSpeed string
	EventsURL       string
	// Shell is the digest of everything the live page cannot swap a slide to
	// change, and is empty for a page nobody is watching.
	Shell string
}

// baseCSS is the stylesheet a theme does not have to write: the mechanics of a
// checkbox and of a coloured run of words, sized from the slide and coloured
// from the variables a theme already sets. The page loads it before the theme's
// own stylesheet, so a theme that wants something else spells it there and wins.
//
// It is here rather than in each theme because a theme is written by hand, and a
// person writing one should not have to know that an input carries a font size
// of its own or that a mask is how a tick is drawn in the theme's own colour.
//
// A brace pair is an action to the template this is written into, so nothing
// here writes the tags a slide uses for a tint.
const baseCSS = `
/* A tint: the span a role colours, written by the tint parser. A theme sets the
   variables rather than spelling these rules again. */
.reveal .tint-accent { color: var(--tint-accent, var(--accent, currentColor)); }
.reveal .tint-muted { color: var(--tint-muted, var(--ink-3, currentColor)); }
.reveal .tint-good { color: var(--tint-good, #2e7d4f); }
.reveal .tint-bad { color: var(--tint-bad, #b23c2f); }

/* Bold, a link and inline code inside a tint take its colour. A theme colours
   each of those itself, and a theme's rule beats an inherited colour, so without
   this a coloured run has a word of body text in the middle of it. */
.reveal .tint-accent *,
.reveal .tint-muted *,
.reveal .tint-good *,
.reveal .tint-bad * {
  color: inherit;
}

/* A task list. Goldmark writes a bare checkbox into the item, which a browser
   draws in the sizes of a form and beside the bullet the theme already paints,
   so the marker comes off and the box is drawn in the sizes of the slide.

   Both shapes of item are named because a list with a blank line in it is a
   loose list, and markdown wraps the contents of a loose item in a paragraph. */
.reveal li:has(> input[type="checkbox"]),
.reveal li:has(> p > input[type="checkbox"]) {
  list-style: none;
}

.reveal li > input[type="checkbox"],
.reveal li > p > input[type="checkbox"] {
  appearance: none;
  /* The em has to come from the slide's text: an input carries a font size of
     its own, and a box in those ems is the size of a checkbox in a form. The
     negative margin hangs it where the bullet would have been, so a task list
     and a plain list start their text on the same column. */
  font: inherit;
  width: 0.58em;
  height: 0.58em;
  margin: 0 0.34em 0 -0.92em;
  border: 2px solid var(--check-box, var(--accent-3, currentColor));
  border-radius: 3px;
  background: transparent;
  print-color-adjust: exact;
  -webkit-print-color-adjust: exact;
}

/* A checked item is the tick alone, drawn by masking the box down to it, which
   is what lets the colour be the theme's rather than one baked into a picture. */
.reveal li > input[type="checkbox"]:checked,
.reveal li > p > input[type="checkbox"]:checked {
  border-color: transparent;
  background: var(--check-mark, var(--accent, currentColor));
  -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath fill='none' stroke='%23000' stroke-width='3' stroke-linecap='round' stroke-linejoin='round' d='M3 8.5l3.5 3.5 6.5-7'/%3E%3C/svg%3E") center / contain no-repeat;
  mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath fill='none' stroke='%23000' stroke-width='3' stroke-linecap='round' stroke-linejoin='round' d='M3 8.5l3.5 3.5 6.5-7'/%3E%3C/svg%3E") center / contain no-repeat;
}
`

// pageTemplate is the whole page. Reveal is initialized with hash: true so a
// reload returns to the slide that was showing, and with the deck's aspect in
// pixels, which every deck passes because reveal's own default is 960 by 700
// rather than any ratio a deck names. The transition and its speed are passed
// for the same reason: reveal's own default is a slide across, and a deck that
// asked for none has to say so. The notes plugin is registered because the
// speaker view is what a slide's notes are for. Every rendering carries the
// escape script, which does nothing until the page finds itself in a frame, so
// an export and a deck under present serve are the same file the board serves.
const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
{{- if .Shell }}
<meta name="` + shellMeta + `" content="{{ .Shell }}">
{{- end }}
<title>{{ .Title }}</title>
<link rel="stylesheet" href="{{ .ResetCSS }}">
<link rel="stylesheet" href="{{ .RevealCSS }}">
<style>
:root {
{{- range .Fonts }}
  --{{ .Name }}: {{ .Stack }};
{{- end }}
}
` + baseCSS + `</style>
<link rel="stylesheet" href="{{ .ThemeCSS }}">
<style>
{{ .Chroma }}</style>
</head>
<body>
<div class="reveal">
<div class="slides">
{{ .Sections }}</div>
</div>
<script src="{{ .RevealJS }}"></script>
<script src="{{ .NotesJS }}"></script>
<script>
Reveal.initialize({
  hash: true,
  width: {{ .Width }},
  height: {{ .Height }},
  transition: "{{ .Transition }}",
  transitionSpeed: "{{ .TransitionSpeed }}",
  plugins: [RevealNotes]
});
</script>
<script>
(function () {
  // A deck at the top of a tab keeps every key to itself. Only a deck the board
  // put in a frame answers to the page around it.
  if (window.top === window.self) {
    return;
  }

  // Reveal binds Escape on document in the bubble phase, so a listener on window
  // in the capture phase runs ahead of it. Reveal's own handler dismisses
  // whatever it has open: its help and preview overlays first, its slide
  // overview second. The event goes through whenever either is up, so the board
  // is asked to close the frame only when the slides are showing, and Escape
  // dismisses what reveal opened before it dismisses the frame around it.
  var revealHasSomethingOpen = function () {
    if (!window.Reveal) {
      return false;
    }

    if (typeof Reveal.isOverlayOpen === "function" && Reveal.isOverlayOpen()) {
      return true;
    }

    return typeof Reveal.isOverview === "function" && Reveal.isOverview();
  };

  // Reveal's jump to slide prompt is an input, and it takes Escape on the input
  // itself to cancel. That binding is on the element rather than on document, so
  // capturing on window still beats it, and swallowing the key closed the frame
  // where the reader meant to dismiss the prompt. Anything a reader types into
  // keeps its own Escape.
  var editing = function (target) {
    if (!target || !target.tagName) {
      return false;
    }

    if (target.isContentEditable) {
      return true;
    }

    var tag = target.tagName.toLowerCase();

    return tag === "input" || tag === "textarea" || tag === "select";
  };

  window.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") {
      return;
    }

    if (editing(event.target) || revealHasSomethingOpen()) {
      return;
    }

    event.preventDefault();
    event.stopPropagation();

    // The board serves this page, so the parent's origin is this page's own and
    // naming it is stricter than "*" for nothing. A deck opened from a file or
    // framed by another site posts to nobody.
    window.parent.postMessage({ planboard: "deck", action: "close" }, window.location.origin);
  }, true);
})();
</script>
{{- if .EventsURL }}
<script>
(function () {
  var reload = function () { location.reload(); };

  var shellOf = function (doc) {
    var meta = doc.querySelector('meta[name="` + shellMeta + `"]');

    return meta === null ? "" : meta.getAttribute("content") || "";
  };

  var slidesOf = function (doc) {
    return doc.querySelector(".reveal .slides");
  };

  var sectionsOf = function (slides) {
    return Array.prototype.filter.call(slides.children, function (child) {
      return child.tagName === "SECTION";
    });
  };

  // The speaker view's notes are posted from this window when reveal says the
  // slide changed. Neither swap moves the index, and reveal dispatches nothing
  // when it has not moved, so the pane would sit on the notes it was last sent.
  // A receiver iframe posts what it is told back to the speaker window, so it
  // says nothing here.
  var announce = function () {
    if (/receiver/i.test(location.search)) {
      return;
    }

    var at = Reveal.getIndices();

    Reveal.dispatchEvent({
      type: "slidechanged",
      data: { indexh: at.h, indexv: at.v, currentSlide: Reveal.getCurrentSlide() }
    });
  };

  var swap = function (doc) {
    var slides = slidesOf(document);
    var fresh = slidesOf(doc);

    if (slides === null || fresh === null) {
      reload();

      return;
    }

    var live = sectionsOf(slides);
    var next = sectionsOf(fresh);

    if (next.length === 0) {
      reload();

      return;
    }

    // Reveal's overview moves the backgrounds element into the slides container,
    // so replacing that container takes every slide background with it, and the
    // handlers the overview binds to each section are not put back by a sync.
    if (Reveal.isOverview()) {
      reload();

      return;
    }

    var structural = live.length !== next.length;

    for (var i = 0; !structural && i < live.length; i++) {
      structural = live[i].getAttribute("data-slide-shell") !== next[i].getAttribute("data-slide-shell");
    }

    document.title = doc.title;

    // A slide added, removed, reordered, or given another page style or
    // background: every section is written again and reveal reads the deck back.
    if (structural) {
      var at = Reveal.getIndices();

      slides.innerHTML = fresh.innerHTML;

      Reveal.sync();
      // The sync above leaves reveal holding the section it was showing, which is
      // no longer in the document, so the slide is selected again.
      Reveal.slide(Math.min(at.h, next.length - 1), at.v);
      announce();

      return;
    }

    var swapped = false;

    for (var j = 0; j < live.length; j++) {
      if (live[j].getAttribute("data-slide-body") === next[j].getAttribute("data-slide-body")) {
        continue;
      }

      live[j].innerHTML = next[j].innerHTML;
      live[j].setAttribute("data-slide-body", next[j].getAttribute("data-slide-body"));

      // One slide rather than Reveal.sync(), which empties the backgrounds
      // element and builds every background again, the showing slide's included.
      // That rebuild is the flash this swap exists to avoid.
      Reveal.syncSlide(live[j]);

      swapped = true;
    }

    if (!swapped) {
      return;
    }

    Reveal.layout();
    announce();
  };

  var fetching = false;
  var pending = false;

  var update = function () {
    if (fetching) {
      pending = true;

      return;
    }

    fetching = true;

    fetch(location.href, { cache: "no-store" }).then(function (answer) {
      if (!answer.ok) {
        throw new Error("the deck answered " + answer.status);
      }

      return answer.text();
    }).then(function (text) {
      var doc = new DOMParser().parseFromString(text, "text/html");
      var shell = shellOf(doc);

      // A digest missing on either side is a page this cannot compare, and one
      // that differs is a theme, an aspect, a transition or a file the page names
      // rather than carries, none of which reveal can be handed while it runs.
      if (shell === "" || shell !== shellOf(document)) {
        reload();

        return;
      }

      swap(doc);
    }).catch(function () {
      // A fetch that failed, a page that answered with an error, a document
      // without slides, or a swap that threw: the page is loaded again, which is
      // what this did for every change before it learned to swap a slide.
      reload();
    }).then(function () {
      fetching = false;

      if (pending) {
        pending = false;
        update();
      }
    });
  };

  var events = new EventSource("{{ .EventsURL }}");
  events.onmessage = update;
  // present serve writes an unnamed event, which onmessage above takes. The
  // board names its change event, and an EventSource hands a named event only
  // to a listener registered for that name.
  events.addEventListener("change", update);
})();
</script>
{{- end }}
</body>
</html>
`

var page = template.Must(template.New("page").Parse(pageTemplate))

func renderPage(show *Presentation, theme *Theme, sections string, stylesheet string, move deckTransition, files string, opts RenderOptions) (string, error) {
	data := pageData{
		Title:           html.EscapeString(show.Title),
		ResetCSS:        opts.assetRef(revealPrefix, revealResetCSS),
		RevealCSS:       opts.assetRef(revealPrefix, revealCoreCSS),
		ThemeCSS:        opts.assetRef(themePrefix, themeStylesheet),
		RevealJS:        opts.assetRef(revealPrefix, revealScript),
		NotesJS:         opts.assetRef(revealPrefix, revealNotesPlug),
		Fonts:           fontProperties(theme.Config.Fonts),
		Chroma:          stylesheet,
		Sections:        sections,
		Width:           show.Width,
		Height:          show.Height,
		Transition:      jsString(move.name),
		TransitionSpeed: jsString(move.speed),
		EventsURL:       jsString(opts.EventsURL),
	}

	if opts.EventsURL != "" {
		data.Shell = shellDigest(data, files)
	}

	var out bytes.Buffer

	err := page.Execute(&out, data)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRenderPage, err)
	}

	return out.String(), nil
}

// assetRef is where the page points at one of its own support files. The prefix
// is trimmed of slashes so a caller that wrote "/static/" still gets a relative
// reference, which is what lets one rendering serve under any path.
func (o RenderOptions) assetRef(dir string, name string) string {
	ref := path.Join(dir, name)

	prefix := strings.Trim(o.AssetPrefix, "/")
	if prefix == "" {
		return ref
	}

	return prefix + "/" + ref
}

// fontProperties are the three font roles as custom properties. A role the
// theme did not name is left out rather than written empty, so the stylesheet's
// own fallback in var(--font-body, serif) still applies.
func fontProperties(fonts ThemeFonts) []fontProperty {
	pairs := []fontProperty{
		{Name: "font-body", Stack: fonts.Body},
		{Name: "font-heading", Stack: fonts.Heading},
		{Name: "font-mono", Stack: fonts.Mono},
	}

	var out []fontProperty

	for _, pair := range pairs {
		if pair.Stack == "" {
			continue
		}

		pair.Stack = cssValue(pair.Stack)
		out = append(out, pair)
	}

	return out
}

// cssValue drops what would end the declaration a font stack is written into. A
// theme.yaml is hand written and a stray brace there would otherwise take the
// rest of the stylesheet with it.
func cssValue(value string) string {
	// A comment opener ends the declaration as surely as a semicolon does: a
	// stack holding one comments out the rest of the block this is written into.
	value = strings.ReplaceAll(value, "/*", "")
	value = strings.ReplaceAll(value, "*/", "")

	return strings.Map(func(r rune) rune {
		switch r {
		case ';', '{', '}', '<', '>', '\n', '\r':
			return -1
		}

		return r
	}, value)
}

// jsString escapes a URL for the JavaScript string literal it is written into.
// The angle brackets go to their escapes because a </script> inside a string
// literal ends the script element the browser is reading.
var jsStringEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"\n", "",
	"\r", "",
	"<", "\\u003c",
	">", "\\u003e",
)

func jsString(value string) string {
	return jsStringEscaper.Replace(value)
}
