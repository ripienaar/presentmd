package present

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadDeck covers a deck that loads cleanly: presentation.yaml in full, the
// slides in number order, and the file that is not a slide passed over.
func TestLoadDeck(t *testing.T) {
	deck := load(t, "testdata/deck")

	for _, problem := range deck.Problems {
		t.Errorf("unexpected problem: %s: %s", problem.Path, problem.Message)
	}

	show := deck.Presentation

	if show.Title != "Planboard" || show.Subtitle != "Planning as markdown" {
		t.Errorf("title %q subtitle %q", show.Title, show.Subtitle)
	}

	if show.Event != "Some Conference" || show.Date != "2026-09-07" {
		t.Errorf("event %q date %q", show.Event, show.Date)
	}

	if show.Theme != "default" || show.Footer != "planboard" {
		t.Errorf("theme %q footer %q", show.Theme, show.Footer)
	}

	if show.Presenter.Name != "R.I." || show.Presenter.Surname != "Pienaar" {
		t.Errorf("presenter was %+v", show.Presenter)
	}

	if show.Presenter.Avatar != "images/avatar.png" || show.Presenter.GitHub != "ripienaar" {
		t.Errorf("presenter was %+v, want the github handle alone", show.Presenter)
	}

	if show.ContactEmail != "rip@devco.net" || show.ContactWeb != "https://devco.net" || show.ContactSocial != "@ripienaar" {
		t.Errorf("contact was %q %q %q", show.ContactEmail, show.ContactWeb, show.ContactSocial)
	}

	if show.ContactSocialNetwork != "Mastodon" {
		t.Errorf("social network was %q, want the network the handle is on", show.ContactSocialNetwork)
	}

	if show.Aspect != "16:9" || show.Width != 1280 || show.Height != 720 {
		t.Errorf("aspect %q is %dx%d, want the 16:9 default", show.Aspect, show.Width, show.Height)
	}

	cases := []struct {
		number     int
		path       string
		pageStyle  string
		caption    string
		cta        string
		notes      string
		background string
		opens      string
	}{
		{number: 1, path: "01-title.md", pageStyle: "title", opens: "# Planboard"},
		{number: 2, path: "03-content.md", pageStyle: "content", caption: "A caption", opens: "# What it is"},
		{number: 3, path: "notes.md", pageStyle: "section", opens: "# The tree"},
		{
			number:     20,
			path:       "20-closing.md",
			pageStyle:  "closing",
			cta:        "Try it",
			notes:      "Thank the room and stop talking.",
			background: "#101820",
			opens:      "# Thanks",
		},
	}

	if len(deck.Slides) != len(cases) {
		t.Fatalf("loaded %d slides, want %d: README.md has no digits and no slide field:\n%s", len(deck.Slides), len(cases), formatSlides(deck.Slides))
	}

	for at, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			slide := deck.Slides[at]

			if slide.Path != tc.path {
				t.Fatalf("slide %d of the deck is %s, want %s: slides come back sorted by number:\n%s", at, slide.Path, tc.path, formatSlides(deck.Slides))
			}

			if slide.Number != tc.number {
				t.Errorf("number %d, want %d", slide.Number, tc.number)
			}

			if slide.PageStyle != tc.pageStyle {
				t.Errorf("page_style %q, want %q", slide.PageStyle, tc.pageStyle)
			}

			if slide.Caption != tc.caption || slide.CTA != tc.cta {
				t.Errorf("caption %q cta %q", slide.Caption, slide.CTA)
			}

			if slide.Notes != tc.notes || slide.Background != tc.background {
				t.Errorf("notes %q background %q", slide.Notes, slide.Background)
			}

			if !strings.HasPrefix(slide.Markdown, tc.opens) {
				t.Errorf("markdown opens with %q, want %q: the body is held as read", firstLine(slide.Markdown), tc.opens)
			}
		})
	}
}

// TestLoadDeckNumberPrecedence pins what the interview settled: the filename
// prefix is the default number and a slide field overrides it, which is how a
// file whose name should not carry a number takes its place in the deck.
func TestLoadDeckNumberPrecedence(t *testing.T) {
	deck := load(t, "testdata/deck")

	cases := []struct {
		path   string
		number int
		why    string
	}{
		{path: "01-title.md", number: 1, why: "the leading digits of the filename"},
		{path: "03-content.md", number: 2, why: "the slide field, over the 03 prefix"},
		{path: "notes.md", number: 3, why: "the slide field, on a name with no digits"},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			slide := slideNamed(deck, tc.path)
			if slide == nil {
				t.Fatalf("no slide %s in the deck:\n%s", tc.path, formatSlides(deck.Slides))
			}

			if slide.Number != tc.number {
				t.Errorf("number %d, want %d from %s", slide.Number, tc.number, tc.why)
			}
		})
	}

	if slideNamed(deck, "README.md") != nil {
		t.Error("README.md loaded as a slide: a file with no digits and no slide field is passed over")
	}
}

func TestLoadDeckProblems(t *testing.T) {
	deck := load(t, "testdata/problems")

	want := []Problem{
		{Path: "presentation.yaml", Message: "contact_socail"},
		{Path: "02-also-first.md", Message: "slide number 1 is already held by 01-first.md"},
		{Path: "03-no-style.md", Message: "missing page_style"},
		{Path: "04-themed.md", Message: "theme is set per presentation, not per slide"},
	}

	for _, tc := range want {
		t.Run(tc.Path+" "+tc.Message, func(t *testing.T) {
			if !hasProblem(deck.Problems, tc.Path, tc.Message) {
				t.Errorf("no problem on %s saying %q, got:\n%s", tc.Path, tc.Message, formatProblems(deck.Problems))
			}
		})
	}

	if len(deck.Problems) != len(want) {
		t.Errorf("found %d problems, want %d:\n%s", len(deck.Problems), len(want), formatProblems(deck.Problems))
	}

	if slideNamed(deck, "03-no-style.md") != nil {
		t.Error("a slide with no page_style entered the deck")
	}

	if slideNamed(deck, "04-themed.md") == nil {
		t.Error("a slide carrying a theme key was left out: the key is a problem, the slide still renders")
	}

	if len(deck.Slides) != 3 {
		t.Errorf("loaded %d slides, want 3: both slides on number 1 stay, the one with no page_style goes:\n%s", len(deck.Slides), formatSlides(deck.Slides))
	}
}

// TestLoadDeckMissingTheme covers the one required field of presentation.yaml.
func TestLoadDeckMissingTheme(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationFile), "title: No theme\n")

	deck := load(t, dir)

	if !hasProblem(deck.Problems, presentationFile, "missing theme") {
		t.Errorf("no problem naming the missing theme, got:\n%s", formatProblems(deck.Problems))
	}
}

// TestLoadDeckAspect covers the default and an explicit ratio, which reveal is
// initialized with because its own default is 960 by 700.
func TestLoadDeckAspect(t *testing.T) {
	cases := []struct {
		dir    string
		aspect string
		width  int
		height int
	}{
		{dir: "testdata/deck", aspect: "16:9", width: 1280, height: 720},
		{dir: "testdata/fourthree", aspect: "4:3", width: 1024, height: 768},
	}

	for _, tc := range cases {
		t.Run(tc.aspect, func(t *testing.T) {
			deck := load(t, tc.dir)

			for _, problem := range deck.Problems {
				t.Errorf("unexpected problem: %s: %s", problem.Path, problem.Message)
			}

			if deck.Presentation.Aspect != tc.aspect {
				t.Errorf("aspect %q, want %q", deck.Presentation.Aspect, tc.aspect)
			}

			if deck.Presentation.Width != tc.width || deck.Presentation.Height != tc.height {
				t.Errorf("aspect %q is %dx%d, want %dx%d", tc.aspect, deck.Presentation.Width, deck.Presentation.Height, tc.width, tc.height)
			}
		})
	}
}

// TestLoadDeckBadAspect covers an aspect that is not a ratio: a problem, and the
// deck falls back to the default rather than handing reveal a zero.
func TestLoadDeckBadAspect(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationFile), "title: Wide\ntheme: default\naspect: widescreen\n")

	deck := load(t, dir)

	if !hasProblem(deck.Problems, presentationFile, `aspect "widescreen" is not a W:H ratio`) {
		t.Errorf("no problem naming the aspect, got:\n%s", formatProblems(deck.Problems))
	}

	if deck.Presentation.Width != 1280 || deck.Presentation.Height != 720 {
		t.Errorf("aspect is %dx%d, want the 16:9 default", deck.Presentation.Width, deck.Presentation.Height)
	}
}

func TestAspectSize(t *testing.T) {
	cases := []struct {
		aspect string
		width  int
		height int
		ok     bool
	}{
		{aspect: "16:9", width: 1280, height: 720, ok: true},
		{aspect: "4:3", width: 1024, height: 768, ok: true},
		{aspect: "16:10", width: 1280, height: 800, ok: true},
		{aspect: "1:1", width: 1280, height: 1280, ok: true},
		{aspect: "16x9"},
		{aspect: "16:0"},
		{aspect: ""},
	}

	for _, tc := range cases {
		t.Run(tc.aspect, func(t *testing.T) {
			width, height, ok := aspectSize(tc.aspect)

			if ok != tc.ok {
				t.Fatalf("parsed %v, want %v", ok, tc.ok)
			}

			if width != tc.width || height != tc.height {
				t.Errorf("got %dx%d, want %dx%d", width, height, tc.width, tc.height)
			}
		})
	}
}

// TestLoadDeckErrors covers the two failures that leave no deck to hold a
// problem against.
func TestLoadDeckErrors(t *testing.T) {
	dir := t.TempDir()

	deck, err := LoadDeck(filepath.Join(dir, "nowhere"))
	if !errors.Is(err, ErrOpenDeck) {
		t.Errorf("opening a directory that is not there returned %v, want ErrOpenDeck", err)
	}
	if deck != nil {
		t.Error("a deck came back alongside the error")
	}

	deck, err = LoadDeck(dir)
	if !errors.Is(err, ErrNoPresentation) {
		t.Errorf("a directory with no presentation.yaml returned %v, want ErrNoPresentation", err)
	}
	if deck != nil {
		t.Error("a deck came back alongside the error")
	}
}

// TestLoadDeckEscapingSlide points a slide at a file outside the deck through an
// absolute symlink. The read goes through the os.Root and is refused, which is
// the confinement every later asset read depends on.
func TestLoadDeckEscapingSlide(t *testing.T) {
	dir := t.TempDir()
	deckDir := filepath.Join(dir, "deck")

	err := os.MkdirAll(deckDir, 0o700)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	writeFile(t, filepath.Join(deckDir, presentationFile), "title: Confined\ntheme: default\n")
	writeFile(t, filepath.Join(deckDir, "01-title.md"), "---\npage_style: title\n---\n\n# Title\n")

	outside := filepath.Join(dir, "outside.md")

	writeFile(t, outside, "---\npage_style: content\n---\n\n# Outside\n")

	err = os.Symlink(outside, filepath.Join(deckDir, "02-escape.md"))
	if err != nil {
		t.Fatalf("symlink: %v", err)
	}

	deck := load(t, deckDir)

	if !hasProblem(deck.Problems, "02-escape.md", "escapes the deck") {
		t.Errorf("no problem naming the escape, got:\n%s", formatProblems(deck.Problems))
	}

	if len(deck.Slides) != 1 || deck.Slides[0].Path != "01-title.md" {
		t.Errorf("the rest of the deck did not load:\n%s", formatSlides(deck.Slides))
	}
}

// TestDeckRoot covers the root a caller reads deck assets through.
func TestDeckRoot(t *testing.T) {
	deck, err := LoadDeck("testdata/deck")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defer deck.Close()

	if deck.Dir != "testdata/deck" {
		t.Errorf("dir %q", deck.Dir)
	}

	if deck.Root() == nil {
		t.Fatal("the deck holds no root: assets are read through it")
	}

	_, err = deck.Root().Open("../load.go")
	if err == nil {
		t.Error("a path leaving the deck opened through the root")
	}
}

func load(t *testing.T, dir string) *Deck {
	t.Helper()

	deck, err := LoadDeck(dir)
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}

	t.Cleanup(func() {
		deck.Close()
	})

	return deck
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func slideNamed(deck *Deck, path string) *Slide {
	for _, slide := range deck.Slides {
		if slide.Path == path {
			return slide
		}
	}

	return nil
}

func hasProblem(problems []Problem, path string, message string) bool {
	for _, problem := range problems {
		if problem.Path == path && strings.Contains(problem.Message, message) {
			return true
		}
	}

	return false
}

func formatProblems(problems []Problem) string {
	lines := make([]string, 0, len(problems))
	for _, problem := range problems {
		lines = append(lines, "  "+problem.Path+": "+problem.Message)
	}

	return strings.Join(lines, "\n")
}

func formatSlides(slides []*Slide) string {
	lines := make([]string, 0, len(slides))
	for _, slide := range slides {
		lines = append(lines, "  "+slide.Path)
	}

	return strings.Join(lines, "\n")
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")

	return line
}

func TestLoadDeckUnparsableYamlStillSized(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationFile), "theme: [\n")

	deck := load(t, dir)

	if !hasProblem(deck.Problems, presentationFile, "cannot parse yaml") {
		t.Errorf("no problem naming the yaml, got:\n%s", formatProblems(deck.Problems))
	}

	// A deck with problems is still served, so reveal has to be handed a slide
	// size rather than a zero pair it would lay every slide out against.
	if deck.Presentation.Width != 1280 || deck.Presentation.Height != 720 {
		t.Errorf("aspect is %dx%d, want the 16:9 default", deck.Presentation.Width, deck.Presentation.Height)
	}
}

func TestAspectSizeRejectsOversizedTerms(t *testing.T) {
	// A term large enough to overflow the multiplication used to come back as a
	// negative height with ok true.
	for _, aspect := range []string{"1:99999999999999999", "99999999999999999:1", "20000:9"} {
		width, height, ok := aspectSize(aspect)
		if ok {
			t.Errorf("aspect %q was accepted as %dx%d", aspect, width, height)
		}
	}
}

func TestLoadDeckEscapingSlideWithoutNumber(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")

	writeFile(t, filepath.Join(dir, presentationFile), "title: Deck\ntheme: default\n")
	writeFile(t, outside, "---\nslide: 1\npage_style: content\n---\n\n# Outside\n")

	err := os.Symlink(outside, filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatalf("cannot link the escaping slide: %v", err)
	}

	deck := load(t, dir)

	// The name carries no number, so nothing but the refusal itself says this
	// file meant to be a slide. Staying quiet would hide the escape.
	if !hasProblem(deck.Problems, "notes.md", "escapes the deck") {
		t.Errorf("no problem naming the escape, got:\n%s", formatProblems(deck.Problems))
	}
}

func TestDeckCloseWithoutRoot(t *testing.T) {
	deck := &Deck{}

	err := deck.Close()
	if err != nil {
		t.Errorf("closing a deck that never loaded returned %v", err)
	}
}

// singleFileDeck is the deck the presentation.md tests read: the presentation as
// frontmatter, a slide for every break, a break inside a fence that is the
// deck's own text, and a thematic break that still divides a columns slide.
const singleFileDeck = "---\n" +
	"title: One File\n" +
	"subtitle: Every slide in it\n" +
	"theme: default\n" +
	"---\n" +
	"\n" +
	"+++\n" +
	"page_style: title\n" +
	"+++\n" +
	"\n" +
	"+++\n" +
	"page_style: content\n" +
	"caption: A caption\n" +
	"cta: Do the thing\n" +
	"text_size: large\n" +
	"+++\n" +
	"\n" +
	"# A Heading\n" +
	"\n" +
	"The body.\n" +
	"\n" +
	"+++\n" +
	"page_style: code\n" +
	"+++\n" +
	"\n" +
	"```markdown\n" +
	"+++\n" +
	"page_style: content\n" +
	"+++\n" +
	"```\n" +
	"\n" +
	"+++\n" +
	"page_style: columns\n" +
	"+++\n" +
	"\n" +
	"Left\n" +
	"\n" +
	"---\n" +
	"\n" +
	"Right\n"

// TestLoadMarkdownDeck covers the whole deck in one file: the presentation from
// the frontmatter, one slide per break in the order they were written, and the
// two things inside a slide that are not a break, a fenced code block holding
// one and the thematic break a columns slide divides at.
func TestLoadMarkdownDeck(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationMarkdownFile), singleFileDeck)

	deck := load(t, dir)

	for _, problem := range deck.Problems {
		t.Errorf("unexpected problem: %s: %s", problem.Path, problem.Message)
	}

	if deck.Source != presentationMarkdownFile {
		t.Errorf("source %q, want %s", deck.Source, presentationMarkdownFile)
	}

	show := deck.Presentation

	if show.Title != "One File" || show.Subtitle != "Every slide in it" || show.Theme != "default" {
		t.Errorf("presentation was %+v", show)
	}

	if show.Aspect != "16:9" || show.Width != 1280 {
		t.Errorf("aspect %q is %d wide, want the default applied as it is for the other form", show.Aspect, show.Width)
	}

	cases := []struct {
		number    int
		path      string
		pageStyle string
		caption   string
		cta       string
		textSize  string
		markdown  string
	}{
		{number: 1, path: "presentation.md:7", pageStyle: "title"},
		{
			number:    2,
			path:      "presentation.md:11",
			pageStyle: "content",
			caption:   "A caption",
			cta:       "Do the thing",
			textSize:  "large",
			markdown:  "# A Heading\n\nThe body.",
		},
		{
			number:    3,
			path:      "presentation.md:22",
			pageStyle: "code",
			markdown:  "```markdown\n+++\npage_style: content\n+++\n```",
		},
		{
			number:    4,
			path:      "presentation.md:32",
			pageStyle: "columns",
			markdown:  "Left\n\n---\n\nRight",
		},
	}

	if len(deck.Slides) != len(cases) {
		t.Fatalf("loaded %d slides, want %d:\n%s", len(deck.Slides), len(cases), formatSlides(deck.Slides))
	}

	for i, want := range cases {
		slide := deck.Slides[i]

		if slide.Number != want.number || slide.Path != want.path {
			t.Errorf("slide %d is number %d at %q, want %d at %q", i, slide.Number, slide.Path, want.number, want.path)
		}

		if slide.PageStyle != want.pageStyle || slide.Caption != want.caption || slide.CTA != want.cta {
			t.Errorf("slide %d: page_style %q caption %q cta %q", i, slide.PageStyle, slide.Caption, slide.CTA)
		}

		if slide.TextSize != want.textSize {
			t.Errorf("slide %d: text_size %q, want %q", i, slide.TextSize, want.textSize)
		}

		if slide.Markdown != want.markdown {
			t.Errorf("slide %d markdown:\n%s\nwant:\n%s", i, slide.Markdown, want.markdown)
		}
	}
}

// TestLoadMarkdownDeckProblems covers what a single file gets wrong, each
// reported against the line the slide opens on rather than against the file.
func TestLoadMarkdownDeckProblems(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationMarkdownFile), "---\n"+
		"title: Problems\n"+
		"theme: default\n"+
		"---\n"+
		"\n"+
		"An orphan paragraph.\n"+
		"\n"+
		"+++\n"+
		"caption: No page style\n"+
		"+++\n"+
		"\n"+
		"# Dropped\n"+
		"\n"+
		"+++\n"+
		"page_style: content\n"+
		"slide: 9\n"+
		"theme: other\n"+
		"+++\n"+
		"\n"+
		"# Kept\n")

	deck := load(t, dir)

	wants := []struct {
		path    string
		message string
	}{
		{path: "presentation.md:6", message: "text above the first +++ belongs to no slide"},
		{path: "presentation.md:8", message: "missing page_style"},
		{path: "presentation.md:14", message: "slide is set by the order in the file"},
		{path: "presentation.md:14", message: "theme is set per presentation"},
	}

	for _, want := range wants {
		if !hasProblem(deck.Problems, want.path, want.message) {
			t.Errorf("no problem %q at %s, got:\n%s", want.message, want.path, formatProblems(deck.Problems))
		}
	}

	// The slide without a page style is the only one dropped, and the one that
	// named a number keeps its place rather than that number.
	if len(deck.Slides) != 1 {
		t.Fatalf("loaded %d slides, want the one that has a page style:\n%s", len(deck.Slides), formatSlides(deck.Slides))
	}

	if deck.Slides[0].Number != 1 {
		t.Errorf("slide number %d, want 1: order in the file is what numbers a slide", deck.Slides[0].Number)
	}
}

// TestLoadMarkdownDeckWithoutFrontmatter covers a file that opens with no
// presentation, which still loads its slides and says what is missing.
func TestLoadMarkdownDeckWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationMarkdownFile), "+++\npage_style: title\n+++\n\n# A Talk\n")

	deck := load(t, dir)

	if !hasProblem(deck.Problems, presentationMarkdownFile, "no yaml frontmatter") {
		t.Errorf("no problem naming the missing frontmatter, got:\n%s", formatProblems(deck.Problems))
	}

	if !hasProblem(deck.Problems, presentationMarkdownFile, "missing theme") {
		t.Errorf("no problem naming the missing theme, got:\n%s", formatProblems(deck.Problems))
	}
}

// TestLoadMarkdownDeckWins covers a directory holding both forms: the single
// file is the deck, and the pair beside it is named rather than half read.
func TestLoadMarkdownDeckWins(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, presentationMarkdownFile), "---\ntitle: The One File\ntheme: default\n---\n\n+++\npage_style: title\n+++\n")
	writeFile(t, filepath.Join(dir, presentationFile), "title: The Pair\ntheme: default\n")
	writeFile(t, filepath.Join(dir, "01-title.md"), "---\npage_style: title\n---\n\n# From the pair\n")

	deck := load(t, dir)

	if deck.Presentation.Title != "The One File" {
		t.Errorf("title %q, want the one presentation.md holds", deck.Presentation.Title)
	}

	if !hasProblem(deck.Problems, presentationMarkdownFile, "presentation.yaml is beside it") {
		t.Errorf("no problem naming the file that is not being read, got:\n%s", formatProblems(deck.Problems))
	}

	if len(deck.Slides) != 1 || deck.Slides[0].Path != "presentation.md:6" {
		t.Fatalf("loaded %d slides, want the one from presentation.md:\n%s", len(deck.Slides), formatSlides(deck.Slides))
	}
}

// TestLoadDeckSourceIsThePair pins the source of a deck written the other way,
// which is what a problem against the presentation names.
func TestLoadDeckSourceIsThePair(t *testing.T) {
	deck := load(t, "testdata/deck")

	if deck.Source != presentationFile {
		t.Errorf("source %q, want %s", deck.Source, presentationFile)
	}

	if deck.presentationPath() != presentationFile {
		t.Errorf("presentation path %q, want %s", deck.presentationPath(), presentationFile)
	}
}

// TestPresentationPathWithoutSource covers a Deck built without LoadDeck, which
// the renderer is handed in tests and by callers that assemble one.
func TestPresentationPathWithoutSource(t *testing.T) {
	deck := &Deck{}

	if deck.presentationPath() != presentationFile {
		t.Errorf("presentation path %q, want %s", deck.presentationPath(), presentationFile)
	}
}
