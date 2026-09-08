package present

import (
	"html"
	"io/fs"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// themeStylesheet is the one stylesheet a theme ships, linked by every page and
// scanned for the url() targets a theme's fonts and images are named by.
const themeStylesheet = "theme.css"

var (
	// assetRefRe matches the three attributes a deck asset is named by in the
	// finished HTML. The list is collected from the rendered sections rather than
	// from the AST because raw HTML passes through: an <img> a person wrote by
	// hand has to be served and inlined like one goldmark rendered, and it never
	// reaches the AST as an image node.
	//
	// golang.org/x/net/html is not a dependency of this module, direct or
	// indirect, so this is a regexp rather than a parse. It reads the attribute
	// forms an HTML5 parser accepts, quoted either way and bare, and what it can
	// be wrong about is a src written inside a comment or a script, which costs a
	// path in the asset list and nothing else.
	// poster carries a video's still and srcset a list of candidates. Both are
	// written by hand on a slide rather than by goldmark, and raw HTML passing
	// through is the reason this reads the finished page at all.
	assetRefRe = regexp.MustCompile("(?i)\\b(?:src|srcset|poster|href|data-background-image|data-background-video)\\s*=\\s*(?:\"([^\"]*)\"|'([^']*)'|([^\\s\"'`=<>]+))")

	// commaListRe says the match came from an attribute holding several paths
	// rather than one, which srcset and a video background both do.
	commaListRe = regexp.MustCompile(`(?i)^\s*(?:srcset|data-background-video)\s*=`)

	// srcsetCandidateRe strips the descriptor a srcset candidate carries after
	// its path, as in "wide.png 2x".
	srcsetCandidateRe = regexp.MustCompile(`\s+\S+$`)

	// cssImportRe matches an @import in a stylesheet. The export does not follow
	// one, and it names a file that would be left as a relative reference, so
	// finding it is how that is reported rather than shipped.
	cssImportRe = regexp.MustCompile(`(?i)@import[^;]*;`)

	// cssURLRe matches a url() target in a stylesheet, which is how a theme's
	// fonts reach the served page and the export.
	cssURLRe = regexp.MustCompile(`(?i)url\(\s*(?:"([^"]*)"|'([^']*)'|([^)'"]*))\s*\)`)

	// schemeRe is the URI scheme grammar. It is what separates https:, mailto:
	// and data: from a path inside the deck, all of which the deck neither serves
	// nor inlines.
	schemeRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

// collectAssets is every file the rendered deck names, as two lists: the paths
// the sections name, which the deck directory answers for, and the url()
// targets of the theme's stylesheet, which the theme does. They are separate
// because they are read through different file systems, and a single list left
// the exporter holding a path with nothing to say which one to open it through.
// Each list is deduplicated and sorted, so an export and a served deck agree on
// it whatever order the slides put the paths in.
func collectAssets(sections string, themeFS fs.FS) ([]string, []string) {
	deck := map[string]struct{}{}

	for _, match := range assetRefRe.FindAllStringSubmatch(sections, -1) {
		if commaListRe.MatchString(match[0]) {
			for _, candidate := range strings.Split(refValue(match), ",") {
				addAsset(deck, srcsetCandidateRe.ReplaceAllString(strings.TrimSpace(candidate), ""))
			}

			continue
		}

		addAsset(deck, refValue(match))
	}

	// An inline style names a background the same way a stylesheet does, and a
	// slide written by hand is where that form turns up.
	for _, match := range cssURLRe.FindAllStringSubmatch(sections, -1) {
		addAsset(deck, refValue(match))
	}

	theme := map[string]struct{}{}

	// A theme with no stylesheet names no fonts. Whether a theme has to ship one
	// is the theme loader's question, not the renderer's.
	css, err := fs.ReadFile(themeFS, themeStylesheet)
	if err == nil {
		for _, match := range cssURLRe.FindAllStringSubmatch(string(css), -1) {
			addAsset(theme, refValue(match))
		}
	}

	return sortedAssets(deck), sortedAssets(theme)
}

func sortedAssets(found map[string]struct{}) []string {
	assets := make([]string, 0, len(found))

	for asset := range found {
		assets = append(assets, asset)
	}

	slices.Sort(assets)

	return assets
}

// deckAssetDir is the path segment the deck's own files are served under. The
// slides write their references relative to the page, so without a segment of
// their own a deck holding a directory called reveal or theme would have those
// requests answered by the vendored reveal or by the theme. Putting the
// references under it is what makes the handler's more specific routes safe.
const deckAssetDir = "assets"

// prefixAssets rewrites every reference in the rendered sections to sit under
// deckAssetDir, leaving alone the ones the deck does not answer for: an absolute
// URL, a data URI, a fragment and a rooted path.
func prefixAssets(sections string) string {
	return rewriteRefs(sections, prefixRef)
}

// rewriteRefs walks every reference of text, in the attributes a deck asset is
// named by and in the url() targets beside them, and replaces the ones rewrite
// answers for. The exporter walks the finished page the same way the renderer
// walks the sections, so the two agree on what a reference is.
func rewriteRefs(text string, rewrite func(string) (string, bool)) string {
	text = assetRefRe.ReplaceAllStringFunc(text, func(match string) string {
		value := refValue(assetRefRe.FindStringSubmatch(match))

		if commaListRe.MatchString(match) {
			candidates := strings.Split(value, ",")

			for i, candidate := range candidates {
				candidates[i] = rewriteCandidate(candidate, rewrite)
			}

			return replaceLast(match, value, strings.Join(candidates, ", "))
		}

		written, ok := rewrite(value)
		if !ok {
			return match
		}

		return replaceLast(match, value, written)
	})

	return rewriteCSSRefs(text, rewrite)
}

// replaceLast puts written where value sits at the end of the match. The value
// is the last thing in every form matched here, since each ends at the closing
// quote or at the end of a bare attribute, while replacing the first occurrence
// rewrote the attribute's own name: href="ref" holds "ref" inside "href", and
// src="s" holds "s" inside "src".
func replaceLast(match string, value string, written string) string {
	at := strings.LastIndex(match, value)
	if at < 0 {
		return match
	}

	return match[:at] + written + match[at+len(value):]
}

// rewriteCSSRefs replaces the url() targets of a stylesheet, which is the whole
// of the rewriting a theme's own stylesheet needs.
func rewriteCSSRefs(text string, rewrite func(string) (string, bool)) string {
	return cssURLRe.ReplaceAllStringFunc(text, func(match string) string {
		value := refValue(cssURLRe.FindStringSubmatch(match))

		written, ok := rewrite(value)
		if !ok {
			return match
		}

		return replaceLast(match, value, written)
	})
}

// rewriteCandidate rewrites one entry of a srcset, which carries a descriptor
// after its path.
func rewriteCandidate(candidate string, rewrite func(string) (string, bool)) string {
	trimmed := strings.TrimSpace(candidate)
	ref := srcsetCandidateRe.ReplaceAllString(trimmed, "")

	written, ok := rewrite(ref)
	if !ok {
		return trimmed
	}

	// The path opens the candidate and the descriptor follows it, so this one is
	// replaced from the front.
	return written + trimmed[len(ref):]
}

// prefixRef answers with the reference as the page should write it, and false
// for one the deck does not serve.
func prefixRef(ref string) (string, bool) {
	_, ok := relativeAsset(ref)
	if !ok {
		return "", false
	}

	return deckAssetDir + "/" + strings.TrimSpace(html.UnescapeString(ref)), true
}

// refValue is the one non-empty group of a match, since the quoted and the bare
// forms are alternatives in the same expression.
func refValue(match []string) string {
	for _, group := range match[1:] {
		if group != "" {
			return group
		}
	}

	return ""
}

func addAsset(found map[string]struct{}, ref string) {
	asset, ok := relativeAsset(ref)
	if !ok {
		return
	}

	found[asset] = struct{}{}
}

// relativeAsset keeps the references the deck directory can answer for and
// drops the rest. An absolute URL, a protocol relative one and a data URI are
// already whole, a fragment points inside the page, and a rooted path is served
// by whatever sits at the root rather than by the deck. What is left is a path
// relative to the page, which under both present serve and the board is the
// deck directory.
func relativeAsset(ref string) (string, bool) {
	ref = strings.TrimSpace(html.UnescapeString(ref))

	switch {
	case ref == "":
		return "", false
	case strings.HasPrefix(ref, "#"):
		return "", false
	case strings.HasPrefix(ref, "//"):
		return "", false
	case strings.HasPrefix(ref, "/"):
		return "", false
	case schemeRe.MatchString(ref):
		return "", false
	case strings.HasPrefix(ref, "../"), strings.Contains(ref, "/../"):
		// The deck root refuses a path that climbs out of the deck, so listing one
		// would queue a read that is always refused and hand the exporter a file it
		// cannot inline.
		return "", false
	}

	// The query and the fragment are the browser's, not the file's: images/a.png#x
	// and images/a.png are one file to serve and one file to inline.
	ref, _, _ = strings.Cut(ref, "#")
	ref, _, _ = strings.Cut(ref, "?")

	if ref == "" {
		return "", false
	}

	// The list holds the file's name rather than the reference's spelling. A
	// slide writing images/one%20two.png names a file called "one two.png", and
	// the request for it arrives decoded, so an encoded entry in the list would
	// refuse a file that is right there.
	decoded, err := url.PathUnescape(ref)
	if err == nil {
		ref = decoded
	}

	return ref, true
}
