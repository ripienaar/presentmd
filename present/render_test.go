package present

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// renderShow is a presentation with enough of presentation.yaml filled in that a
// theme places a footer and a title, and no more.
func renderShow() *Presentation {
	return &Presentation{
		Title:     "Rendering",
		Subtitle:  "A deck through a theme",
		Theme:     "default",
		Footer:    "presentmd",
		Presenter: Presenter{Name: "R.I.", Surname: "Pienaar"},
		Width:     1280,
		Height:    720,
	}
}

// renderSlides renders slides the test wrote rather than a deck on disk. A Deck
// built here holds no root, which is what Close allows for, and the renderer
// reads nothing through one.
func renderSlides(t *testing.T, theme *Theme, show *Presentation, slides ...*Slide) *RenderedDeck {
	t.Helper()

	deck := &Deck{Dir: "testdata", Presentation: *show, Slides: slides}

	out, err := RenderDeck(deck, theme, RenderOptions{})
	if err != nil {
		t.Fatalf("the deck did not render: %v", err)
	}

	return out
}

// sectionFor is the one <section> of a page rendered from a single slide.
func sectionFor(t *testing.T, out *RenderedDeck) string {
	t.Helper()

	start := strings.Index(out.HTML, "<section")
	if start < 0 {
		t.Fatalf("the page holds no section, problems are %v", out.Problems)
	}

	end := strings.Index(out.HTML[start:], "</section>")
	if end < 0 {
		t.Fatal("a section was opened and never closed")
	}

	return out.HTML[start : start+end]
}

func noProblems(t *testing.T, out *RenderedDeck) {
	t.Helper()

	if len(out.Problems) != 0 {
		t.Fatalf("problems %v", out.Problems)
	}
}

// TestRenderDeckSectionPerPageStyle pins that every slide of a loaded deck
// becomes one section carrying its page style twice: the class a theme's CSS
// selects on and the reveal state a slide can be styled by.
func TestRenderDeckSectionPerPageStyle(t *testing.T) {
	deck, err := LoadDeck(deckDir)
	if err != nil {
		t.Fatalf("cannot load the deck fixture: %v", err)
	}
	defer deck.Close()

	out, err := RenderDeck(deck, loadDefaultTheme(t), RenderOptions{})
	if err != nil {
		t.Fatalf("the deck did not render: %v", err)
	}

	noProblems(t, out)

	if strings.Count(out.HTML, "<section") != len(deck.Slides) {
		t.Fatalf("%d sections for %d slides", strings.Count(out.HTML, "<section"), len(deck.Slides))
	}

	at := 0

	for _, slide := range deck.Slides {
		want := `<section class="slide-` + slide.PageStyle + `" data-state="` + slide.PageStyle + `"`

		found := strings.Index(out.HTML[at:], want)
		if found < 0 {
			t.Fatalf("%s is not a section in slide order, want %s", slide.Path, want)
		}

		at += found + len(want)
	}
}

// TestRenderDeckEveryRipienaarStyle renders one slide through each of the
// theme's page styles, so a style that renders only when a template happens to
// be exercised by the deck fixture is not left untried.
func TestRenderDeckEveryRipienaarStyle(t *testing.T) {
	theme := loadDefaultTheme(t)

	for _, style := range theme.Styles() {
		t.Run(style, func(t *testing.T) {
			out := renderSlides(t, theme, renderShow(), &Slide{
				Path:      "01-" + style + ".md",
				PageStyle: style,
				Caption:   "A caption",
				CTA:       "A call to action",
				Markdown:  "# A heading\n\nA body.\n",
			})

			noProblems(t, out)

			if !strings.Contains(out.HTML, `<section class="slide-`+style+`" data-state="`+style+`"`) {
				t.Errorf("%s did not become a section: %s", style, out.HTML)
			}
		})
	}
}

// TestRenderDeckLiftsTheFirstHeading pins the interview's rule that a slide's own
// level-one heading is the heading the theme places, and that a slide without
// one is all body.
func TestRenderDeckLiftsTheFirstHeading(t *testing.T) {
	theme := loadDefaultTheme(t)

	lifted := renderSlides(t, theme, renderShow(), &Slide{
		Path:      "01-lift.md",
		PageStyle: "content",
		Markdown:  "# What it is\n\nA binary over markdown.\n\n## A deeper heading\n",
	})

	noProblems(t, lifted)

	section := sectionFor(t, lifted)

	if !strings.Contains(section, `<h1 class="slide-heading">What it is</h1>`) {
		t.Errorf("the heading was not lifted into the theme's own h1: %s", section)
	}

	body := section[strings.Index(section, `<div class="body">`):]

	if strings.Contains(body, "<h1") {
		t.Error("the lifted heading is still in the body, so the slide shows it twice")
	}

	// A deeper heading is the author writing a heading inside their slide.
	if !strings.Contains(body, "A deeper heading") {
		t.Error("a level-two heading was lifted as well")
	}

	plain := renderSlides(t, theme, renderShow(), &Slide{
		Path:      "02-plain.md",
		PageStyle: "content",
		Markdown:  "Just a body.\n",
	})

	noProblems(t, plain)

	section = sectionFor(t, plain)

	if strings.Contains(section, "slide-heading") {
		t.Error("a slide with no level-one heading was given one")
	}

	if !strings.Contains(section, "Just a body.") {
		t.Error("the body of a slide with no heading went missing")
	}
}

// TestRenderDeckSplitsASplitStyle pins that a style named in split_styles divides
// at its first thematic break, in order, with no rule drawn where the break was.
func TestRenderDeckSplitsASplitStyle(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Path:      "01-columns.md",
		PageStyle: "columns",
		Markdown:  "# Two halves\n\nThe left half.\n\n---\n\nThe right half.\n",
	})

	noProblems(t, out)

	section := sectionFor(t, out)

	left := strings.Index(section, "The left half.")
	right := strings.Index(section, "The right half.")

	if left < 0 || right < 0 {
		t.Fatalf("a half is missing: %s", section)
	}

	if left > right {
		t.Error("the halves are the wrong way round")
	}

	if strings.Contains(section, "<hr") {
		t.Error("the break the author split on was also drawn as a rule")
	}
}

// TestRenderDeckReportsASecondThematicBreak pins that a split style holds one
// break, and that the slide holding two is left out while the slides around it
// are not.
func TestRenderDeckReportsASecondThematicBreak(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(),
		&Slide{
			Path:      "01-columns.md",
			PageStyle: "columns",
			Markdown:  "Left.\n\n---\n\nMiddle.\n\n---\n\nRight.\n",
		},
		&Slide{
			Path:      "02-content.md",
			PageStyle: "content",
			Markdown:  "The slide beside it.\n",
		},
	)

	if len(out.Problems) != 1 {
		t.Fatalf("problems %v, want the second break alone", out.Problems)
	}

	if out.Problems[0].Path != "01-columns.md" {
		t.Errorf("the problem is against %q", out.Problems[0].Path)
	}

	if !strings.Contains(out.Problems[0].Message, "thematic break") {
		t.Errorf("the problem reads %q", out.Problems[0].Message)
	}

	if strings.Contains(out.HTML, "Middle.") {
		t.Error("the slide with two breaks was rendered anyway")
	}

	if !strings.Contains(out.HTML, "The slide beside it.") {
		t.Error("a slide with a problem took the slide beside it out of the deck")
	}
}

// TestRenderDeckKeepsABreakOutsideASplitStyle pins the other half of the rule: a
// --- in a style the theme does not split is the rule it is written as.
func TestRenderDeckKeepsABreakOutsideASplitStyle(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "Above the rule.\n\n---\n\nBelow the rule.\n",
	})

	noProblems(t, out)

	section := sectionFor(t, out)

	if !strings.Contains(section, "<hr") {
		t.Errorf("the break was swallowed rather than drawn: %s", section)
	}

	if !strings.Contains(section, "Above the rule.") || !strings.Contains(section, "Below the rule.") {
		t.Error("the body was split in a style that does not split")
	}
}

// TestRenderDeckLeavesAWikiLinkLiteral pins that the board's [[target]]
// extension is not part of this goldmark: a deck is not in the planning tree and
// has nothing to resolve a target against.
func TestRenderDeckLeavesAWikiLinkLiteral(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "See [[a link]] for the rest.\n",
	})

	noProblems(t, out)

	section := sectionFor(t, out)

	if !strings.Contains(section, "[[a link]]") {
		t.Errorf("the wiki link was rewritten: %s", section)
	}

	if strings.Contains(section, "<a ") {
		t.Error("the wiki link became an anchor")
	}
}

// TestRenderDeckPassesRawHTMLAndCollectsItsAssets pins both halves of the raw
// HTML decision: a hand-written img is the image it was written as, and it
// reaches the asset list, which is why the list is collected from the rendered
// HTML rather than from the AST.
func TestRenderDeckPassesRawHTMLAndCollectsItsAssets(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown: "<img src=\"images/hand.png\" alt=\"hand written\">\n\n" +
			"![rendered](images/goldmark.png)\n\n" +
			"[elsewhere](https://example.net/away.png)\n\n" +
			"[down the page](#later)\n",
	})

	noProblems(t, out)

	section := sectionFor(t, out)

	if !strings.Contains(section, `<img src="assets/images/hand.png" alt="hand written">`) {
		t.Errorf("the hand-written img was escaped rather than passed through: %s", section)
	}

	assets := strings.Join(out.Assets, ",")

	for _, want := range []string{"images/hand.png", "images/goldmark.png"} {
		if !strings.Contains(assets, want) {
			t.Errorf("assets %v do not hold %q", out.Assets, want)
		}
	}

	for _, unwanted := range []string{"example.net", "#later"} {
		if strings.Contains(assets, unwanted) {
			t.Errorf("assets %v hold %q, which the deck does not serve", out.Assets, unwanted)
		}
	}
}

// TestRenderDeckBackgroundAttribute pins which of reveal's two background
// attributes a value means, since the same field carries both a color and a
// path.
func TestRenderDeckBackgroundAttribute(t *testing.T) {
	cases := []struct {
		name       string
		background string
		want       string
	}{
		{name: "a hex color", background: "#101820", want: ` data-background-color="#101820"`},
		{name: "a css color name", background: "navy", want: ` data-background-color="navy"`},
		{name: "a color name a short list would miss", background: "papayawhip", want: ` data-background-color="papayawhip"`},
		{name: "a path", background: "images/backdrop.png", want: ` data-background-image="assets/images/backdrop.png"`},
		{name: "nothing", background: "", want: ""},
	}

	theme := loadDefaultTheme(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderSlides(t, theme, renderShow(), &Slide{
				Path:       "01-content.md",
				PageStyle:  "content",
				Background: tc.background,
				Markdown:   "A body.\n",
			})

			noProblems(t, out)

			section := sectionFor(t, out)
			open := section[:strings.Index(section, ">")+1]

			if tc.want == "" {
				if strings.Contains(open, "data-background") {
					t.Errorf("a slide with no background got %q", open)
				}

				return
			}

			if !strings.Contains(open, tc.want) {
				t.Errorf("section opens %q, want %q in it", open, tc.want)
			}
		})
	}

	// A background image is a file the deck serves, so it belongs in the list
	// beside the images the markdown named.
	out := renderSlides(t, theme, renderShow(), &Slide{
		Path:       "01-content.md",
		PageStyle:  "content",
		Background: "images/backdrop.png",
		Markdown:   "A body.\n",
	})

	if !strings.Contains(strings.Join(out.Assets, ","), "images/backdrop.png") {
		t.Errorf("assets %v do not hold the background image", out.Assets)
	}
}

// TestRenderDeckWritesTheNotesAside pins that notes reach the speaker view,
// which is the aside reveal's notes plugin reads, rendered as the markdown it
// was written as.
func TestRenderDeckWritesTheNotesAside(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Notes:     "Thank the room and **stop talking**.",
		Markdown:  "A body.\n",
	})

	noProblems(t, out)

	section := sectionFor(t, out)

	if !strings.Contains(section, `<aside class="notes">`) {
		t.Fatalf("the notes are not in an aside: %s", section)
	}

	if !strings.Contains(section, "<strong>stop talking</strong>") {
		t.Error("the notes were placed as text rather than rendered as markdown")
	}
}

// TestRenderDeckInlineCaptionAndCTA pins that the two frontmatter lines a theme
// places inside its own element are rendered without the paragraph goldmark
// would otherwise wrap them in.
func TestRenderDeckInlineCaptionAndCTA(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Caption:   "A **bold** caption",
		CTA:       "Try `planboard`",
		Markdown:  "A body.\n",
	})

	noProblems(t, out)

	section := sectionFor(t, out)

	if !strings.Contains(section, `<div class="slide-caption">A <strong>bold</strong> caption</div>`) {
		t.Errorf("the caption is not inline markdown: %s", section)
	}

	if !strings.Contains(section, `<div class="slide-cta">Try <code>planboard</code></div>`) {
		t.Errorf("the call to action is not inline markdown: %s", section)
	}
}

// TestRenderDeckHighlightsCodeOnce pins that fenced code is colored by classes
// with line numbers, and that the stylesheet those classes read is written once
// for the page rather than once per block.
func TestRenderDeckHighlightsCodeOnce(t *testing.T) {
	source := "```go\nfunc main() {\n\tprintln(\"hello\")\n}\n```\n"

	out := renderSlides(t, loadDefaultTheme(t), renderShow(),
		&Slide{Path: "01-code.md", PageStyle: "code", Markdown: source},
		&Slide{Path: "02-code.md", PageStyle: "code", Markdown: source},
	)

	noProblems(t, out)

	section := sectionFor(t, out)

	if !strings.Contains(section, `class="chroma"`) {
		t.Fatalf("the code block was not highlighted: %s", section)
	}

	if !strings.Contains(section, `<span class="kd">func</span>`) {
		t.Error("chroma wrote inline styles rather than the classes the stylesheet names")
	}

	if !strings.Contains(section, `class="ln"`) {
		t.Error("the code block carries no line numbers")
	}

	// Chroma's own comment for the wrapper rule, which the stylesheet holds one
	// of however many blocks the deck has.
	if count := strings.Count(out.HTML, "/* PreWrapper */"); count != 1 {
		t.Errorf("the code stylesheet is in the page %d times, want once", count)
	}
}

// TestRenderDeckReportsAnUnknownPageStyle pins that a page style the theme has
// no template for is a problem against the slide that named it, and that the
// slide leaves the deck rather than the deck failing.
func TestRenderDeckReportsAnUnknownPageStyle(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(),
		&Slide{Path: "01-odd.md", PageStyle: "sideways", Markdown: "Sideways.\n"},
		&Slide{Path: "02-content.md", PageStyle: "content", Markdown: "The slide beside it.\n"},
	)

	if len(out.Problems) != 1 {
		t.Fatalf("problems %v, want the unknown style alone", out.Problems)
	}

	if out.Problems[0].Path != "01-odd.md" {
		t.Errorf("the problem is against %q", out.Problems[0].Path)
	}

	if !strings.Contains(out.Problems[0].Message, "sideways") {
		t.Errorf("the problem reads %q, want the style it named", out.Problems[0].Message)
	}

	if strings.Contains(out.HTML, "Sideways.") {
		t.Error("a slide naming a style the theme has no template for was rendered")
	}

	if !strings.Contains(out.HTML, "The slide beside it.") {
		t.Error("the slide beside the bad one left the deck too")
	}

	// A partial is not a page style, so a slide naming one is naming a style that
	// does not exist.
	out = renderSlides(t, loadDefaultTheme(t), renderShow(),
		&Slide{Path: "01-partial.md", PageStyle: "_footer", Markdown: "A body.\n"},
	)

	if len(out.Problems) != 1 {
		t.Errorf("a slide naming a partial gave problems %v", out.Problems)
	}
}

// TestRenderDeckGitHubAvatar pins the handle check. The URL is built rather than
// fetched, so a handle that cannot be one has to be caught here or it reaches a
// title slide as a broken image.
func TestRenderDeckGitHubAvatar(t *testing.T) {
	cases := []struct {
		name    string
		handle  string
		avatar  string
		problem bool
	}{
		{name: "a handle", handle: "ripienaar", avatar: "https://github.com/ripienaar.png"},
		{name: "hyphens inside", handle: "choria-io", avatar: "https://github.com/choria-io.png"},
		{name: "a leading hyphen", handle: "-ripienaar", problem: true},
		{name: "a trailing hyphen", handle: "ripienaar-", problem: true},
		{name: "a path", handle: "ripienaar/planboard", problem: true},
		{name: "a url", handle: "https://github.com/ripienaar", problem: true},
		{name: "too long", handle: strings.Repeat("a", 40), problem: true},
		{name: "the longest handle", handle: strings.Repeat("a", 39), avatar: "https://github.com/" + strings.Repeat("a", 39) + ".png"},
	}

	theme := loadDefaultTheme(t)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			show := renderShow()
			show.Presenter.GitHub = tc.handle

			out := renderSlides(t, theme, show, &Slide{
				Path:      "01-title.md",
				PageStyle: "title",
				Markdown:  "# A talk\n",
			})

			if tc.problem {
				if len(out.Problems) != 1 {
					t.Fatalf("problems %v, want the handle alone", out.Problems)
				}

				if out.Problems[0].Path != "presentation.yaml" {
					t.Errorf("the problem is against %q", out.Problems[0].Path)
				}

				if strings.Contains(out.HTML, "title-avatar") {
					t.Error("an avatar was placed for a handle that is not one")
				}

				return
			}

			noProblems(t, out)

			if !strings.Contains(out.HTML, tc.avatar) {
				t.Errorf("the page does not carry %q", tc.avatar)
			}

			// The avatar is fetched by the browser, not by the renderer, so it is
			// not a file the deck serves.
			if strings.Contains(strings.Join(out.Assets, ","), "github.com") {
				t.Errorf("assets %v hold the avatar URL", out.Assets)
			}
		})
	}

	// An avatar the presentation named wins, so the handle is never turned into a
	// URL and never checked.
	show := renderShow()
	show.Presenter.GitHub = "-not-a-handle-"
	show.Presenter.Avatar = "images/avatar.png"

	out := renderSlides(t, theme, show, &Slide{Path: "01-title.md", PageStyle: "title", Markdown: "# A talk\n"})

	noProblems(t, out)

	if !strings.Contains(strings.Join(out.Assets, ","), "images/avatar.png") {
		t.Errorf("assets %v do not hold the avatar the presentation named", out.Assets)
	}
}

// TestRenderDeckThemeAssetsHoldAThemeFont pins the second list: the url()
// targets in the theme's stylesheet, which is how a theme's fonts are served
// live and inlined on export. They are apart from the deck's own because the
// exporter opens the two through different file systems.
func TestRenderDeckThemeAssetsHoldAThemeFont(t *testing.T) {
	theme, err := LoadTheme("../theme-fixture", deckDir)
	if err != nil {
		t.Fatalf("cannot load the fixture theme: %v", err)
	}

	show := renderShow()
	show.Theme = "../theme-fixture"

	out := renderSlides(t, theme, show, &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "A body.\n",
	})

	noProblems(t, out)

	if !strings.Contains(strings.Join(out.ThemeAssets, ","), "fonts/fixture-sans.woff2") {
		t.Errorf("theme assets %v do not hold the font the theme's css names", out.ThemeAssets)
	}

	// The font is the theme's file and the deck root cannot open it, which is the
	// whole reason the two lists are apart.
	if slices.Contains(out.Assets, "fonts/fixture-sans.woff2") {
		t.Errorf("assets %v hold a file the theme answers for", out.Assets)
	}
}

// TestRenderDeckPage pins what the assembled page hands reveal and how it names
// its own files. The references are relative because one rendering serves at /
// under present serve and under /present/<project>/<deck>/ on the board.
func TestRenderDeckPage(t *testing.T) {
	deck, err := LoadDeck(deckDir)
	if err != nil {
		t.Fatalf("cannot load the deck fixture: %v", err)
	}
	defer deck.Close()

	out, err := RenderDeck(deck, loadDefaultTheme(t), RenderOptions{})
	if err != nil {
		t.Fatalf("the deck did not render: %v", err)
	}

	noProblems(t, out)

	want := []string{
		"hash: true",
		"width: 1280",
		"height: 720",
		"plugins: [RevealNotes]",
		`<link rel="stylesheet" href="reveal/reset.css">`,
		`<link rel="stylesheet" href="reveal/reveal.css">`,
		`<link rel="stylesheet" href="theme/theme.css">`,
		`<script src="reveal/reveal.js"></script>`,
		`<script src="reveal/plugin/notes.js"></script>`,
		`--font-body: "IBM Plex Sans"`,
		`--font-heading: "IBM Plex Sans Condensed"`,
		`--font-mono: "IBM Plex Mono"`,
	}

	for _, fragment := range want {
		if !strings.Contains(out.HTML, fragment) {
			t.Errorf("the page does not carry %q", fragment)
		}
	}

	if strings.Contains(out.HTML, `href="/`) || strings.Contains(out.HTML, `src="/`) {
		t.Error("the page names a file from the root, so it breaks under a path prefix")
	}

	if strings.Contains(out.HTML, "EventSource") {
		t.Error("a rendering with no events URL carries a reload script")
	}
}

// TestRenderDeckPageOptions pins the two fields a caller sets: where the reload
// script listens, and where the page's own support files are served from.
func TestRenderDeckPageOptions(t *testing.T) {
	deck, err := LoadDeck(deckDir)
	if err != nil {
		t.Fatalf("cannot load the deck fixture: %v", err)
	}
	defer deck.Close()

	out, err := RenderDeck(deck, loadDefaultTheme(t), RenderOptions{EventsURL: "events", AssetPrefix: "/static/"})
	if err != nil {
		t.Fatalf("the deck did not render: %v", err)
	}

	if !strings.Contains(out.HTML, `new EventSource("events")`) {
		t.Error("the reload script does not listen where it was told to")
	}

	if !strings.Contains(out.HTML, `<script src="static/reveal/reveal.js"></script>`) {
		t.Error("the asset prefix did not reach the reveal reference")
	}

	if !strings.Contains(out.HTML, `<link rel="stylesheet" href="static/theme/theme.css">`) {
		t.Error("the asset prefix did not reach the theme reference")
	}

	// A prefix written with a leading slash is still used relative, since an
	// absolute reference is what breaks a deck served under a path prefix.
	if strings.Contains(out.HTML, `src="/static`) {
		t.Error("the asset prefix was written as an absolute path")
	}
}

// TestRenderDeckFramedEscape pins the script the board's overlay needs. Reveal
// binds Escape to the slide overview on document, so a page in a frame has to
// take the key in the capture phase and hand it to the board itself. Every
// rendering carries the script: it does nothing until the page finds itself in a
// frame, and the board serves the same file present serve and the exporter do.
func TestRenderDeckFramedEscape(t *testing.T) {
	deck, err := LoadDeck(deckDir)
	if err != nil {
		t.Fatalf("cannot load the deck fixture: %v", err)
	}
	defer deck.Close()

	want := []string{
		"window.top === window.self",
		`window.addEventListener("keydown"`,
		// Both of reveal's own answers to Escape. Checking the overview alone let
		// Escape close the frame out from under the help overlay, which reveal
		// dismisses on the same key.
		"Reveal.isOverlayOpen()",
		"Reveal.isOverview()",
		// Reveal's jump to slide prompt is an input that cancels on Escape, and
		// swallowing the key closed the frame instead of the prompt.
		"editing(event.target)",
		"event.stopPropagation()",
		`window.parent.postMessage({ planboard: "deck", action: "close" }, window.location.origin)`,
	}

	// The first is what present serve and the exporter render, the second what
	// the board does: its own events endpoint and its own asset prefix.
	renders := map[string]RenderOptions{
		"a plain rendering": {},
		"the board's":       {EventsURL: "../../events", AssetPrefix: "/static/"},
	}

	for name, opts := range renders {
		out, err := RenderDeck(deck, loadDefaultTheme(t), opts)
		if err != nil {
			t.Fatalf("the deck did not render: %v", err)
		}

		for _, fragment := range want {
			if !strings.Contains(out.HTML, fragment) {
				t.Errorf("%s page does not carry %q", name, fragment)
			}
		}

		// Reveal's own handler is bound on document in the bubble phase, so the
		// capture argument is the whole of why this listener runs first.
		if !strings.Contains(out.HTML, "}, true);") {
			t.Errorf("%s page binds the escape listener without the capture phase", name)
		}
	}
}

// TestRenderDeckRefusesNothing pins the two arguments the renderer cannot work
// without, which are errors rather than problems because there is no deck to
// hold a problem against.
func TestRenderDeckRefusesNothing(t *testing.T) {
	_, err := RenderDeck(nil, loadDefaultTheme(t), RenderOptions{})
	if err == nil {
		t.Error("a nil deck rendered")
	}

	_, err = RenderDeck(&Deck{}, nil, RenderOptions{})
	if err == nil {
		t.Error("a nil theme rendered")
	}
}

// TestRenderDeckExtensions pins the extension set. Deleting any of Table,
// Strikethrough, Linkify, TaskList, Footnote or the auto heading ids left every
// other test green, so the set that spec section 5 opens with was carried by
// nothing.
func TestRenderDeckExtensions(t *testing.T) {
	theme := loadDefaultTheme(t)

	markdown := strings.Join([]string{
		"| a | b |",
		"| --- | --- |",
		"| 1 | 2 |",
		"",
		"~~struck~~",
		"",
		"https://example.net/plain",
		"",
		"- [ ] unchecked",
		"- [x] checked",
		"",
		"## A Sub Heading",
		"",
		"A note[^1].",
		"",
		"[^1]: The note.",
	}, "\n")

	out := renderSlides(t, theme, renderShow(), &Slide{Number: 1, Path: "01.md", PageStyle: "content", Markdown: markdown})
	noProblems(t, out)

	section := sectionFor(t, out)

	cases := map[string]string{
		"tables":           "<table>",
		"strikethrough":    "<del>struck</del>",
		"linkify":          `<a href="https://example.net/plain"`,
		"task lists":       `type="checkbox"`,
		"footnotes":        "footnote",
		"auto heading ids": `id="s1-a-sub-heading"`,
	}

	for extension, want := range cases {
		if !strings.Contains(section, want) {
			t.Errorf("%s: the section does not carry %q", extension, want)
		}
	}
}

// TestRenderDeckIDsAreUniqueAcrossSlides pins that ids carry the slide they came
// from. Every slide lands in one page, so two slides sharing a heading shared an
// id, and two slides each using a footnote both wrote fn:1, which sent the
// second slide's footnote link to the first slide's note.
func TestRenderDeckIDsAreUniqueAcrossSlides(t *testing.T) {
	theme := loadDefaultTheme(t)

	markdown := "## Setup\n\nA note[^1].\n\n[^1]: The note.\n"

	out := renderSlides(t, theme, renderShow(),
		&Slide{Number: 1, Path: "01.md", PageStyle: "content", Markdown: markdown},
		&Slide{Number: 2, Path: "02.md", PageStyle: "content", Markdown: markdown},
	)
	noProblems(t, out)

	for _, id := range []string{`id="s1-setup"`, `id="s2-setup"`} {
		if strings.Count(out.HTML, id) != 1 {
			t.Errorf("%s appears %d times, want once", id, strings.Count(out.HTML, id))
		}
	}

	if strings.Count(out.HTML, `id="fn:1"`) > 1 {
		t.Error("two slides wrote the same footnote id, so one slide's link reaches the other's note")
	}
}

// TestRenderDeckHighlightsAFenceWithNoLanguage pins that an unlabeled fence gets
// the same frame as a labeled one. Without a lexer the block rendered as a bare
// pre with no wrapper and no line numbers, and a terminal transcript on a slide
// is written exactly that way.
func TestRenderDeckHighlightsAFenceWithNoLanguage(t *testing.T) {
	theme := loadDefaultTheme(t)

	out := renderSlides(t, theme, renderShow(), &Slide{
		Number:    1,
		Path:      "01.md",
		PageStyle: "code",
		Markdown:  "```\n# ccm apply obj://CCM/nats.tgz\nWARN service refreshed\n```\n",
	})
	noProblems(t, out)

	section := sectionFor(t, out)

	if !strings.Contains(section, "chroma") {
		t.Error("an unlabeled fence rendered outside chroma's wrapper")
	}

	if !strings.Contains(section, `class="ln"`) && !strings.Contains(section, `class="lnt"`) {
		t.Error("an unlabeled fence rendered without line numbers")
	}
}

// TestRenderDeckAssetsFromHandWrittenHTML pins the forms a slide written by hand
// uses that goldmark never emits. Raw HTML passing through is the reason the
// list is scraped from the finished page rather than the tree.
func TestRenderDeckAssetsFromHandWrittenHTML(t *testing.T) {
	theme := loadDefaultTheme(t)

	markdown := strings.Join([]string{
		`<div style="background-image: url(images/inline.png)">boxed</div>`,
		"",
		`<img srcset="images/wide.png 2x, images/narrow.png 1x" src="images/plain.png">`,
		"",
		`<video poster="images/still.png"></video>`,
		"",
		`<img src="../escape.png">`,
	}, "\n")

	out := renderSlides(t, theme, renderShow(), &Slide{Number: 1, Path: "01.md", PageStyle: "content", Markdown: markdown})

	for _, want := range []string{"images/inline.png", "images/wide.png", "images/narrow.png", "images/plain.png", "images/still.png"} {
		if !slices.Contains(out.Assets, want) {
			t.Errorf("assets %v do not hold %q", out.Assets, want)
		}
	}

	// The deck root refuses a path that climbs out of the deck, so listing one
	// queues a read that is always refused and an export that cannot inline it.
	if slices.Contains(out.Assets, "../escape.png") {
		t.Errorf("assets %v hold a path that leaves the deck", out.Assets)
	}
}

// transitionTheme is a theme naming a transition of its own. The embedded theme
// does not serve for this: it names none, which is also the renderer's default,
// so a theme's value reaching the page cannot be told from the default.
func transitionTheme(t *testing.T, transition string) *Theme {
	t.Helper()

	dir := writeTheme(t, map[string]string{
		"theme.yaml":         "code_style: bw\ntransition: " + transition + "\n",
		"styles/content.jet": `<div class="body">{{ raw: body }}</div>` + "\n",
	})

	theme, err := LoadTheme(dir, deckDir)
	if err != nil {
		t.Fatalf("cannot load a theme naming transition %q: %v", transition, err)
	}

	return theme
}

// noTransitionTheme is a theme naming no transition, which is what a deck
// deciding for itself renders through. It is a directory rather than the
// embedded theme so that the theme's silence is the test's own, and stays so
// when the embedded theme changes its mind.
func noTransitionTheme(t *testing.T) *Theme {
	t.Helper()

	dir := writeTheme(t, map[string]string{
		"theme.yaml": "code_style: bw\nfonts:\n  body: Helvetica, Arial, sans-serif\n  heading: Helvetica, Arial, sans-serif\n  mono: Menlo, Consolas, monospace\n",
		"styles/content.jet": `{{ if heading != "" }}<h2>{{ raw: heading }}</h2>{{ end }}` + "\n" +
			`{{ raw: body }}` + "\n" +
			`{{ if caption != "" }}<p class="caption">{{ raw: caption }}</p>{{ end }}` + "\n" +
			`{{ include "_footer.jet" }}` + "\n",
		"styles/title.jet": `<h1>{{ raw: heading }}</h1>` + "\n" +
			`{{ if presentation.Subtitle != "" }}<p class="subtitle">{{ presentation.Subtitle }}</p>{{ end }}` + "\n",
		"styles/_footer.jet": `{{ if len(footer) > 0 }}<p class="deck-footer">{{ range i, part := footer }}{{ if i > 0 }} | {{ end }}{{ part }}{{ end }}</p>{{ end }}` + "\n",
	})

	theme, err := LoadTheme(dir, deckDir)
	if err != nil {
		t.Fatalf("cannot load a theme naming no transition: %v", err)
	}

	return theme
}

// openingTags are the <section ...> tags of a page, in slide order, so a test
// can say which slide carries an attribute rather than only that the page does.
func openingTags(t *testing.T, out *RenderedDeck) []string {
	t.Helper()

	var tags []string

	for _, part := range strings.Split(out.HTML, "<section")[1:] {
		end := strings.Index(part, ">")
		if end < 0 {
			t.Fatal("a section was opened and never closed")
		}

		tags = append(tags, part[:end])
	}

	return tags
}

// TestRenderDeckTransitionDefaultsToNone pins the value a deck gets when neither
// it nor its theme names one. Reveal's own default is a slide across, so a page
// that passed nothing would animate a deck that asked for nothing.
func TestRenderDeckTransitionDefaultsToNone(t *testing.T) {
	out := renderSlides(t, noTransitionTheme(t), renderShow(), &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "A body.\n",
	})

	noProblems(t, out)

	for _, want := range []string{`transition: "none"`, `transitionSpeed: "default"`} {
		if !strings.Contains(out.HTML, want) {
			t.Errorf("the page does not carry %q", want)
		}
	}

	if strings.Contains(out.HTML, "data-transition") {
		t.Error("a slide that named no transition got one on its section")
	}
}

// TestRenderDeckTransitionDeckWinsOverTheme pins the precedence: a deck that
// names a transition moves that way through a theme that names another.
func TestRenderDeckTransitionDeckWinsOverTheme(t *testing.T) {
	show := renderShow()
	show.Transition = "fade"

	out := renderSlides(t, transitionTheme(t, "zoom"), show, &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "A body.\n",
	})

	noProblems(t, out)

	if !strings.Contains(out.HTML, `transition: "fade"`) {
		t.Error("the theme's transition beat the deck's own")
	}
}

// TestRenderDeckTransitionFromTheme pins the other half: the theme's feel
// applies to a deck that names no transition of its own.
func TestRenderDeckTransitionFromTheme(t *testing.T) {
	out := renderSlides(t, transitionTheme(t, "zoom"), renderShow(), &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "A body.\n",
	})

	noProblems(t, out)

	if !strings.Contains(out.HTML, `transition: "zoom"`) {
		t.Error("the theme's transition did not reach a deck that named none")
	}
}

// TestRenderDeckTransitionSpeed pins that the speed is the deck's alone and
// reaches the page beside the transition it belongs to.
func TestRenderDeckTransitionSpeed(t *testing.T) {
	show := renderShow()
	show.Transition = "fade"
	show.TransitionSpeed = "fast"

	out := renderSlides(t, noTransitionTheme(t), show, &Slide{
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "A body.\n",
	})

	noProblems(t, out)

	if !strings.Contains(out.HTML, `transitionSpeed: "fast"`) {
		t.Errorf("the page does not carry the speed the deck asked for")
	}
}

// TestRenderDeckSlideTransition pins reveal's per-slide override: it lands on
// the section of the slide that asked for it and on no other, and the deck
// around it still moves the way the presentation says.
func TestRenderDeckSlideTransition(t *testing.T) {
	out := renderSlides(t, noTransitionTheme(t), renderShow(),
		&Slide{Path: "01-content.md", PageStyle: "content", Transition: "zoom", Markdown: "The landing.\n"},
		&Slide{Path: "02-content.md", PageStyle: "content", Markdown: "The slide beside it.\n"},
	)

	noProblems(t, out)

	tags := openingTags(t, out)
	if len(tags) != 2 {
		t.Fatalf("%d sections, want one per slide", len(tags))
	}

	if !strings.Contains(tags[0], ` data-transition="zoom"`) {
		t.Errorf("the slide that named a transition opens %q", tags[0])
	}

	if strings.Contains(tags[1], "data-transition") {
		t.Errorf("the slide beside it opens %q, want no transition of its own", tags[1])
	}

	if !strings.Contains(out.HTML, `transition: "none"`) {
		t.Error("a slide's own transition became the deck's")
	}
}

// TestRenderDeckReportsABadTransition pins that a value reveal does not know is
// a problem against the file that wrote it, and that the deck renders with what
// it would have had without the line rather than handing reveal a word it drops
// in silence.
func TestRenderDeckReportsABadTransition(t *testing.T) {
	t.Run("presentation.yaml", func(t *testing.T) {
		show := renderShow()
		show.Transition = "dissolve"

		out := renderSlides(t, noTransitionTheme(t), show, &Slide{
			Path:      "01-content.md",
			PageStyle: "content",
			Markdown:  "A body.\n",
		})

		if len(out.Problems) != 1 {
			t.Fatalf("problems %v, want the transition alone", out.Problems)
		}

		if out.Problems[0].Path != presentationFile {
			t.Errorf("the problem is against %q", out.Problems[0].Path)
		}

		if !strings.Contains(out.Problems[0].Message, "dissolve") {
			t.Errorf("the problem reads %q, want the value it named", out.Problems[0].Message)
		}

		if !strings.Contains(out.HTML, `transition: "none"`) {
			t.Error("the page carries a transition reveal does not know")
		}
	})

	t.Run("transition_speed", func(t *testing.T) {
		show := renderShow()
		show.TransitionSpeed = "brisk"

		out := renderSlides(t, noTransitionTheme(t), show, &Slide{
			Path:      "01-content.md",
			PageStyle: "content",
			Markdown:  "A body.\n",
		})

		if len(out.Problems) != 1 {
			t.Fatalf("problems %v, want the speed alone", out.Problems)
		}

		if out.Problems[0].Path != presentationFile {
			t.Errorf("the problem is against %q", out.Problems[0].Path)
		}

		if !strings.Contains(out.HTML, `transitionSpeed: "default"`) {
			t.Error("the page carries a speed reveal does not know")
		}
	})

	t.Run("theme.yaml", func(t *testing.T) {
		theme := transitionTheme(t, "dissolve")

		out := renderSlides(t, theme, renderShow(), &Slide{
			Path:      "01-content.md",
			PageStyle: "content",
			Markdown:  "A body.\n",
		})

		if len(out.Problems) != 1 {
			t.Fatalf("problems %v, want the theme's transition alone", out.Problems)
		}

		// A theme that will not load takes the whole deck down, which a transition
		// nobody can spell has no business doing, so the theme loaded and the value
		// is a problem against its theme.yaml.
		if out.Problems[0].Path != theme.Name+"/"+themeConfigFile {
			t.Errorf("the problem is against %q, want the theme's own theme.yaml", out.Problems[0].Path)
		}

		if !strings.Contains(out.HTML, `transition: "none"`) {
			t.Error("the page carries a transition reveal does not know")
		}
	})

	t.Run("a slide", func(t *testing.T) {
		out := renderSlides(t, noTransitionTheme(t), renderShow(),
			&Slide{Path: "01-content.md", PageStyle: "content", Transition: "dissolve", Markdown: "The odd slide.\n"},
			&Slide{Path: "02-content.md", PageStyle: "content", Markdown: "The slide beside it.\n"},
		)

		if len(out.Problems) != 1 {
			t.Fatalf("problems %v, want the slide's transition alone", out.Problems)
		}

		if out.Problems[0].Path != "01-content.md" {
			t.Errorf("the problem is against %q", out.Problems[0].Path)
		}

		if strings.Contains(out.HTML, "data-transition") {
			t.Error("a transition reveal does not know reached the section")
		}

		// The slide moves the way the deck does rather than leaving the deck: a
		// misspelled transition is not a reason to drop a slide from a talk.
		if !strings.Contains(out.HTML, "The odd slide.") {
			t.Error("a slide naming a transition reveal does not know was left out")
		}
	})
}

// TestRenderDeckTextSizeDefaultsToNothing pins that a slide at the normal size
// carries no attribute, whether it named normal or named nothing at all, so a
// theme's own sizing applies with nothing overriding it.
func TestRenderDeckTextSizeDefaultsToNothing(t *testing.T) {
	out := renderSlides(t, noTransitionTheme(t), renderShow(),
		&Slide{Path: "01-content.md", PageStyle: "content", Markdown: "A body.\n"},
		&Slide{Path: "02-content.md", PageStyle: "content", TextSize: "normal", Markdown: "The slide beside it.\n"},
	)

	noProblems(t, out)

	if strings.Contains(out.HTML, "data-text-size") {
		t.Error("a slide at the normal size got a text size on its section")
	}
}

// TestRenderDeckSlideTextSize pins that each step lands on the section of the
// slide that asked for it and on no other, since the step is the slide's alone
// and a deck has no size of its own to spread.
func TestRenderDeckSlideTextSize(t *testing.T) {
	for _, size := range []string{"small", "large", "huge"} {
		t.Run(size, func(t *testing.T) {
			out := renderSlides(t, noTransitionTheme(t), renderShow(),
				&Slide{Path: "01-content.md", PageStyle: "content", TextSize: size, Markdown: "The full slide.\n"},
				&Slide{Path: "02-content.md", PageStyle: "content", Markdown: "The slide beside it.\n"},
			)

			noProblems(t, out)

			tags := openingTags(t, out)
			if len(tags) != 2 {
				t.Fatalf("%d sections, want one per slide", len(tags))
			}

			if !strings.Contains(tags[0], ` data-text-size="`+size+`"`) {
				t.Errorf("the slide that named %q opens %q", size, tags[0])
			}

			if strings.Contains(tags[1], "data-text-size") {
				t.Errorf("the slide beside it opens %q, want no size of its own", tags[1])
			}
		})
	}
}

// TestRenderDeckReportsABadTextSize pins that a step no theme carries is a
// problem against the slide that wrote it, and that the slide still renders at
// the normal size rather than leaving the talk.
func TestRenderDeckReportsABadTextSize(t *testing.T) {
	out := renderSlides(t, noTransitionTheme(t), renderShow(),
		&Slide{Path: "01-content.md", PageStyle: "content", TextSize: "enormous", Markdown: "The odd slide.\n"},
		&Slide{Path: "02-content.md", PageStyle: "content", Markdown: "The slide beside it.\n"},
	)

	if len(out.Problems) != 1 {
		t.Fatalf("problems %v, want the slide's text size alone", out.Problems)
	}

	if out.Problems[0].Path != "01-content.md" {
		t.Errorf("the problem is against %q", out.Problems[0].Path)
	}

	if !strings.Contains(out.Problems[0].Message, "enormous") {
		t.Errorf("the problem reads %q, want the value it named", out.Problems[0].Message)
	}

	if strings.Contains(out.HTML, "data-text-size") {
		t.Error("a size no theme carries reached the section")
	}

	if !strings.Contains(out.HTML, "The odd slide.") {
		t.Error("a slide naming a size no theme carries was left out")
	}
}

// TestEmbeddedThemesCarryEveryTextSize pins that the shipped theme styles every
// step a slide may name. A step a theme spells nothing for renders at the normal
// size and the author is told nothing, since the value is one the renderer
// accepts.
func TestEmbeddedThemesCarryEveryTextSize(t *testing.T) {
	for _, theme := range []*Theme{loadDefaultTheme(t)} {
		t.Run(theme.Name, func(t *testing.T) {
			css, err := fs.ReadFile(theme.FS(), themeStylesheet)
			if err != nil {
				t.Fatalf("cannot read the stylesheet: %v", err)
			}

			for _, size := range []string{"small", "large", "huge"} {
				if !strings.Contains(string(css), `data-text-size="`+size+`"`) {
					t.Errorf("the stylesheet carries no rule for %q", size)
				}
			}

			// normal is the theme's own sizing, which a rule keyed on an attribute no
			// slide carries would never reach.
			if strings.Contains(string(css), `data-text-size="normal"`) {
				t.Error("the stylesheet carries a rule for normal, which no slide is written with")
			}
		})
	}
}

// TestCSSValueClosesNoBlock pins that a font stack cannot end the declaration it
// is written into, comment openers included, which is the failure the escaping
// exists to prevent.
func TestCSSValueClosesNoBlock(t *testing.T) {
	got := cssValue("Gill Sans/* ; } body { display:none")

	for _, bad := range []string{"/*", "*/", ";", "{", "}"} {
		if strings.Contains(got, bad) {
			t.Errorf("cssValue kept %q in %q", bad, got)
		}
	}
}

// TestRenderDeckRewritesAReferenceWhoseValueIsInItsAttributeName pins the
// rewriting against the case that corrupted it: the value was put where it first
// appeared in the match, and href="ref" holds "ref" inside "href" while src="s"
// holds "s" inside "src".
func TestRenderDeckRewritesAReferenceWhoseValueIsInItsAttributeName(t *testing.T) {
	theme := loadDefaultTheme(t)

	markdown := strings.Join([]string{
		`<img src="s">`,
		"",
		`<a href="ref">a link</a>`,
		"",
		`<img src="image" data-background-image="image">`,
	}, "\n")

	out := renderSlides(t, theme, renderShow(), &Slide{Number: 1, Path: "01.md", PageStyle: "content", Markdown: markdown})

	section := sectionFor(t, out)

	for _, want := range []string{`src="assets/s"`, `href="assets/ref"`, `src="assets/image"`} {
		if !strings.Contains(section, want) {
			t.Errorf("the section does not carry %s, got:\n%s", want, section)
		}
	}

	for _, bad := range []string{"assets/src=", "assets/href=", "<img assets", "<a assets"} {
		if strings.Contains(section, bad) {
			t.Errorf("the rewrite corrupted the markup with %q:\n%s", bad, section)
		}
	}
}

// livePage renders the deck at dir through theme as a page that is being
// watched, which is the only rendering that carries the digests.
func livePage(t *testing.T, dir string, theme *Theme) string {
	t.Helper()

	out, err := RenderDeck(load(t, dir), theme, RenderOptions{EventsURL: "events"})
	if err != nil {
		t.Fatalf("the deck did not render: %v", err)
	}

	return out.HTML
}

// shellOf is the digest the page carries for everything a slide swap cannot
// change.
func shellOf(t *testing.T, page string) string {
	t.Helper()

	open := `<meta name="planboard-shell" content="`

	at := strings.Index(page, open)
	if at < 0 {
		t.Fatal("the page carries no shell digest")
	}

	rest := page[at+len(open):]

	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("the shell digest is not closed")
	}

	return rest[:end]
}

// slideDigests are the two digests of every section, in the order the sections
// are written.
func slideDigests(t *testing.T, page string) []string {
	t.Helper()

	var found []string

	for _, part := range strings.Split(page, "<section ")[1:] {
		end := strings.Index(part, ">")
		if end < 0 {
			t.Fatal("a section tag is not closed")
		}

		attributes := part[:end]

		for _, name := range []string{"data-slide-shell", "data-slide-body"} {
			open := name + `="`

			at := strings.Index(attributes, open)
			if at < 0 {
				t.Fatalf("a section carries no %s: %s", name, attributes)
			}

			rest := attributes[at+len(open):]
			found = append(found, name+":"+rest[:strings.Index(rest, `"`)])
		}
	}

	if len(found) == 0 {
		t.Fatal("the page holds no sections")
	}

	return found
}

// TestRenderDeckDigestsOnlyForALivePage covers the gate on all of it. A page
// nobody is watching is what the exporter renders, and it carries neither the
// shell digest nor the two on every section.
func TestRenderDeckDigestsOnlyForALivePage(t *testing.T) {
	theme := loadDefaultTheme(t)

	out, err := RenderDeck(load(t, deckDir), theme, RenderOptions{})
	if err != nil {
		t.Fatalf("the deck did not render: %v", err)
	}

	for _, unwanted := range []string{"planboard-shell", "data-slide-shell", "data-slide-body"} {
		if strings.Contains(out.HTML, unwanted) {
			t.Errorf("a page nobody is watching carries %s", unwanted)
		}
	}

	page := livePage(t, deckDir, theme)

	for _, want := range []string{"planboard-shell", "data-slide-shell", "data-slide-body"} {
		if !strings.Contains(page, want) {
			t.Errorf("a live page does not carry %s", want)
		}
	}
}

// TestRenderDeckDigestsAreStable is what keeps the board from swapping every
// slide on every change: it renders a deck on every request, and a digest that
// moved on its own would tell the page the whole deck had changed.
func TestRenderDeckDigestsAreStable(t *testing.T) {
	theme := loadDefaultTheme(t)

	first := livePage(t, deckDir, theme)
	second := livePage(t, deckDir, theme)

	if shellOf(t, first) != shellOf(t, second) {
		t.Error("two renders of one deck carry different shell digests")
	}

	if !slices.Equal(slideDigests(t, first), slideDigests(t, second)) {
		t.Error("two renders of one deck carry different slide digests")
	}
}

// TestRenderDeckShellDigestFollowsTheThemeStylesheet covers the case the shell
// digest exists for: a stylesheet the page loads rather than carries cannot be
// handed to a reveal that is already running, so the page reloads instead.
func TestRenderDeckShellDigestFollowsTheThemeStylesheet(t *testing.T) {
	dir := t.TempDir()

	err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "theme-fixture")))
	if err != nil {
		t.Fatalf("cannot copy the theme fixture: %v", err)
	}

	load := func() *Theme {
		theme, err := LoadTheme(dir, dir)
		if err != nil {
			t.Fatalf("cannot load the theme: %v", err)
		}

		return theme
	}

	before := shellOf(t, livePage(t, deckDir, load()))

	writeFile(t, filepath.Join(dir, "theme.css"), "body { color: rebeccapurple }")

	if shellOf(t, livePage(t, deckDir, load())) == before {
		t.Error("the shell digest did not follow the theme stylesheet")
	}
}

// TestRenderDeckShellDigestFollowsADeckAsset covers the image case. Editing a
// slide's image renders a page that is byte identical, so nothing on it says
// the image changed and the digest is the only thing that does.
func TestRenderDeckShellDigestFollowsADeckAsset(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationFile), "title: A Talk\ntheme: default\n")
	writeFile(t, filepath.Join(dir, "01-image.md"), "---\npage_style: content\n---\n\n# Shown\n\n![it](picture.png)\n")
	writeFile(t, filepath.Join(dir, "picture.png"), "the first picture")

	theme := loadDefaultTheme(t)

	before := shellOf(t, livePage(t, dir, theme))

	writeFile(t, filepath.Join(dir, "picture.png"), "a picture of another size")

	if shellOf(t, livePage(t, dir, theme)) == before {
		t.Error("the shell digest did not follow a deck asset")
	}
}

// TestRenderDeckSlideDigestsSplitAttributesFromBody is the split the swap turns
// on: a slide whose text changed keeps its attribute digest, and only that
// slide's contents are put in place. A slide that changed its background is
// written again as a whole.
func TestRenderDeckSlideDigestsSplitAttributesFromBody(t *testing.T) {
	theme := loadDefaultTheme(t)
	show := renderShow()

	sectionOf := func(slide *Slide) string {
		out, err := RenderDeck(&Deck{Dir: "testdata", Presentation: *show, Slides: []*Slide{slide}}, theme, RenderOptions{EventsURL: "events"})
		if err != nil {
			t.Fatalf("the deck did not render: %v", err)
		}

		noProblems(t, out)

		return sectionFor(t, out)
	}

	first := sectionOf(&Slide{Number: 1, Path: "01.md", PageStyle: "content", Markdown: "# Heading\n\nThe body as written."})
	edited := sectionOf(&Slide{Number: 1, Path: "01.md", PageStyle: "content", Markdown: "# Heading\n\nThe body after an edit."})
	backed := sectionOf(&Slide{Number: 1, Path: "01.md", PageStyle: "content", Markdown: "# Heading\n\nThe body as written.", Background: "#101010"})

	shell := func(section string) string {
		return strings.Split(strings.Split(section, `data-slide-shell="`)[1], `"`)[0]
	}

	body := func(section string) string {
		return strings.Split(strings.Split(section, `data-slide-body="`)[1], `"`)[0]
	}

	if shell(first) != shell(edited) {
		t.Error("editing a slide's text changed its attribute digest, which writes the whole section again")
	}

	if body(first) == body(edited) {
		t.Error("editing a slide's text left its body digest, so the page would not swap it")
	}

	if shell(first) == shell(backed) {
		t.Error("a slide that changed its background kept its attribute digest")
	}
}
