package present

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// deckDir is what LoadTheme resolves a relative directory theme against. The
// deck it names holds no theme of its own, which is the point: a directory theme
// sits beside the deck or nowhere near it.
const deckDir = "testdata/deck"

// writeTheme builds a theme directory under t.TempDir from a path to contents
// map. The themes built this way are the ones that fail to load, which have no
// place in testdata beside a fixture somebody might copy.
func writeTheme(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	// theme.css is required of every theme, so a case about something else is
	// given one rather than failing on the stylesheet before reaching its subject.
	_, given := files[themeStyleFile]
	if !given {
		files[themeStyleFile] = ".reveal { color: black; }\n"
	}

	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))

		err := os.MkdirAll(filepath.Dir(full), 0o755)
		if err != nil {
			t.Fatalf("cannot create %s: %v", filepath.Dir(full), err)
		}

		err = os.WriteFile(full, []byte(body), 0o644)
		if err != nil {
			t.Fatalf("cannot write %s: %v", full, err)
		}
	}

	return dir
}

// TestLoadThemeEmbedded pins the lookup for a name that is not a path: it finds
// the theme built into the binary, and the theme comes back with its yaml
// decoded and its templates parsed.
func TestLoadThemeEmbedded(t *testing.T) {
	theme, err := LoadTheme("default", deckDir)
	if err != nil {
		t.Fatalf("cannot load the embedded default theme: %v", err)
	}

	if theme.Name != "default" || theme.Dir != "" {
		t.Errorf("name %q dir %q, want the embedded theme to name no directory", theme.Name, theme.Dir)
	}

	if theme.Config.CodeStyle != "github" {
		t.Errorf("code_style %q", theme.Config.CodeStyle)
	}

	if theme.Config.Fonts.Body == "" || theme.Config.Fonts.Heading == "" || theme.Config.Fonts.Mono == "" {
		t.Errorf("fonts were %+v, want all three stacks", theme.Config.Fonts)
	}

	styles := theme.Styles()
	if strings.Join(styles, ",") != "closing,code,columns,content,image,section,title" {
		t.Errorf("styles %v", styles)
	}

	css, err := theme.FS().Open("theme.css")
	if err != nil {
		t.Fatalf("the theme's own files are not reachable through FS: %v", err)
	}
	css.Close()

	// An include resolves when the template runs, so an embed pattern that
	// dropped the partial would load and then fail on the first content slide.
	data := NewTemplateData(&Presentation{Footer: "planboard"})
	data.Slide = &Slide{PageStyle: "content"}
	data.Body = "<p>A deck is a directory.</p>"

	out, err := theme.Render("content", data)
	if err != nil {
		t.Fatalf("cannot render the embedded content style: %v", err)
	}

	if !strings.Contains(out, "planboard") {
		t.Errorf("content rendered as %q, want the embedded partial's footer", out)
	}
}

// TestLoadThemeRelativeDirectory pins the other half of the rule: a name holding
// a path separator is a directory, resolved against the deck directory, and the
// path may leave the deck.
func TestLoadThemeRelativeDirectory(t *testing.T) {
	theme, err := LoadTheme("../theme-fixture", deckDir)
	if err != nil {
		t.Fatalf("cannot load the fixture theme by relative path: %v", err)
	}

	want := filepath.Join(deckDir, "..", "theme-fixture")
	if theme.Dir != want {
		t.Errorf("dir %q, want %q", theme.Dir, want)
	}

	styles := theme.Styles()
	if strings.Join(styles, ",") != "closing,columns,content,title" {
		t.Errorf("styles %v", styles)
	}

	if !theme.Splits("columns") {
		t.Error("columns is in split_styles and does not split")
	}

	if theme.Splits("content") {
		t.Error("content is not in split_styles and splits")
	}
}

// TestLoadThemeAbsoluteDirectoryOutsideDeck loads a theme by absolute path from
// a directory that is neither the deck nor anywhere under the planning root. The
// binary follows that path deliberately, so that two decks can share one theme,
// and a change that starts confining directory themes fails here.
func TestLoadThemeAbsoluteDirectoryOutsideDeck(t *testing.T) {
	dir := writeTheme(t, map[string]string{
		"theme.yaml":         "code_style: bw\n",
		"theme.css":          ".reveal { color: black; }\n",
		"styles/content.jet": "<h2>{{ raw: heading }}</h2>\n",
	})

	if !filepath.IsAbs(dir) {
		t.Fatalf("the temporary theme path %q is not absolute", dir)
	}

	theme, err := LoadTheme(dir, deckDir)
	if err != nil {
		t.Fatalf("cannot load a theme outside the deck: %v", err)
	}

	if theme.Dir != dir {
		t.Errorf("dir %q, want the absolute path %q unchanged", theme.Dir, dir)
	}

	if !theme.HasStyle("content") {
		t.Errorf("styles %v, want content", theme.Styles())
	}
}

// TestLoadThemeUnknownName pins the error for a name that is neither an embedded
// theme nor a path, which names the themes that do exist because that is the
// answer the person needs.
func TestLoadThemeUnknownName(t *testing.T) {
	_, err := LoadTheme("fancy", deckDir)
	if !errors.Is(err, ErrUnknownTheme) {
		t.Fatalf("error was %v, want ErrUnknownTheme", err)
	}

	if !strings.Contains(err.Error(), "default") {
		t.Errorf("error %q does not name the embedded themes", err)
	}
}

// TestLoadThemeMissingDirectory pins that a path nothing sits at is reported as
// the directory it is, rather than as a missing theme.yaml.
func TestLoadThemeMissingDirectory(t *testing.T) {
	_, err := LoadTheme("./nowhere", deckDir)
	if !errors.Is(err, ErrOpenTheme) {
		t.Fatalf("error was %v, want ErrOpenTheme", err)
	}
}

// TestPartialIsNotAPageStyle pins the underscore rule from both sides: the
// partial is not a style a slide can name, and the style that includes it still
// gets its output.
func TestPartialIsNotAPageStyle(t *testing.T) {
	theme := fixtureTheme(t)

	for _, style := range theme.Styles() {
		if strings.HasPrefix(style, "_") {
			t.Errorf("style %q is a partial and is offered as a page style", style)
		}
	}

	if theme.HasStyle("_footer") {
		t.Error("_footer is a partial and HasStyle claims it")
	}

	_, err := theme.Render("_footer", NewTemplateData(&Presentation{}))
	if !errors.Is(err, ErrNoPageStyle) {
		t.Fatalf("rendering a partial gave %v, want ErrNoPageStyle", err)
	}

	data := NewTemplateData(&Presentation{Footer: "planboard"})
	data.Slide = &Slide{PageStyle: "content"}

	out, err := theme.Render("content", data)
	if err != nil {
		t.Fatalf("cannot render content: %v", err)
	}

	if !strings.Contains(out, `<p class="deck-footer">planboard</p>`) {
		t.Errorf("content rendered as %q, want the included partial's footer", out)
	}
}

// TestSplitStyleWithoutTemplate pins that split_styles is checked against the
// templates the theme actually has, so a renamed style is caught when the theme
// loads rather than on the columns slide that comes out unsplit.
func TestSplitStyleWithoutTemplate(t *testing.T) {
	dir := writeTheme(t, map[string]string{
		"theme.yaml":         "split_styles:\n  - columns\n",
		"styles/content.jet": "<h2>{{ raw: heading }}</h2>\n",
	})

	_, err := LoadTheme(dir, deckDir)
	if !errors.Is(err, ErrThemeConfig) {
		t.Fatalf("error was %v, want ErrThemeConfig", err)
	}

	if !strings.Contains(err.Error(), "columns") {
		t.Errorf("error %q does not name the style", err)
	}
}

// TestBrokenTemplateFailsAtLoad pins that every template under styles/ is parsed
// when the theme loads, partials included, so a syntax error is a theme that
// will not load rather than a deck that fails on one slide mid-talk.
func TestBrokenTemplateFailsAtLoad(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "page style",
			files: map[string]string{
				"theme.yaml":         "code_style: bw\n",
				"styles/content.jet": "<h2>{{ if }}</h2>\n",
			},
		},
		{
			name: "partial",
			files: map[string]string{
				"theme.yaml":         "code_style: bw\n",
				"styles/content.jet": "<h2>{{ raw: heading }}</h2>\n",
				"styles/_broken.jet": "{{ range }}\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeTheme(t, tc.files)

			_, err := LoadTheme(dir, deckDir)
			if !errors.Is(err, ErrThemeTemplate) {
				t.Fatalf("error was %v, want ErrThemeTemplate", err)
			}
		})
	}
}

// TestThemeRender pins the variable names a template reaches, which are the
// contract a directory theme this repository never sees is written against, and
// the escaping that goes with them: the rendered HTML is written raw and a
// frontmatter value is escaped.
func TestThemeRender(t *testing.T) {
	theme := fixtureTheme(t)

	show := &Presentation{
		Title:         "Planboard",
		Subtitle:      "Planning & markdown",
		Presenter:     Presenter{Name: "R.I.", Surname: "Pienaar", Avatar: "images/avatar.png"},
		ContactEmail:  "rip@devco.net",
		ContactWeb:    "https://devco.net",
		ContactSocial: "@ripienaar",
	}

	title := NewTemplateData(show)
	title.Slide = &Slide{PageStyle: "title"}
	title.Heading = "<em>Planboard</em>"

	out, err := theme.Render("title", title)
	if err != nil {
		t.Fatalf("cannot render title: %v", err)
	}

	if !strings.Contains(out, "<h1><em>Planboard</em></h1>") {
		t.Errorf("title rendered as %q, want the heading HTML written raw", out)
	}

	if !strings.Contains(out, "Planning &amp; markdown") {
		t.Errorf("title rendered as %q, want the subtitle escaped", out)
	}

	if !strings.Contains(out, `<p class="presenter">R.I. Pienaar</p>`) {
		t.Errorf("title rendered as %q, want the presenter line", out)
	}

	if !strings.Contains(out, `src="images/avatar.png"`) {
		t.Errorf("title rendered as %q, want the avatar", out)
	}

	content := NewTemplateData(show)
	content.Slide = &Slide{PageStyle: "content"}
	content.Heading = "What it is"
	content.Body = "<p>A deck is a directory.</p>"
	content.Caption = "<em>from the deck</em>"

	out, err = theme.Render("content", content)
	if err != nil {
		t.Fatalf("cannot render content: %v", err)
	}

	for _, want := range []string{
		"<h2>What it is</h2>",
		"<p>A deck is a directory.</p>",
		`<p class="caption"><em>from the deck</em></p>`,
		`<p class="style">content</p>`,
		`<p class="deck-footer">R.I. Pienaar | rip@devco.net | https://devco.net | @ripienaar</p>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("content rendered as %q, want %q in it", out, want)
		}
	}

	columns := NewTemplateData(show)
	columns.Slide = &Slide{PageStyle: "columns"}
	columns.Heading = "Two halves"
	columns.Left = "<p>left</p>"
	columns.Right = "<p>right</p>"

	out, err = theme.Render("columns", columns)
	if err != nil {
		t.Fatalf("cannot render columns: %v", err)
	}

	if !strings.Contains(out, `<div class="left"><p>left</p></div>`) || !strings.Contains(out, `<div class="right"><p>right</p></div>`) {
		t.Errorf("columns rendered as %q, want both halves", out)
	}
}

// TestRenderUnknownStyle pins the error a slide naming a style the theme does
// not have turns into.
func TestRenderUnknownStyle(t *testing.T) {
	theme := fixtureTheme(t)

	_, err := theme.Render("quote", NewTemplateData(&Presentation{}))
	if !errors.Is(err, ErrNoPageStyle) {
		t.Fatalf("error was %v, want ErrNoPageStyle", err)
	}

	if !strings.Contains(err.Error(), "quote") {
		t.Errorf("error %q does not name the style", err)
	}
}

func fixtureTheme(t *testing.T) *Theme {
	t.Helper()

	theme, err := LoadTheme("../theme-fixture", deckDir)
	if err != nil {
		t.Fatalf("cannot load the fixture theme: %v", err)
	}

	return theme
}

func TestFixtureExercisesContactsAndCTA(t *testing.T) {
	// contacts and cta reached a template through no fixture until a page style
	// ranged over contacts and got the loop index rather than the Contact. The
	// twelve names are a contract themes outside this repository are written
	// against, so each one renders here rather than only appearing in a map.
	theme, err := LoadTheme("../theme-fixture", deckDir)
	if err != nil {
		t.Fatalf("cannot load the fixture theme: %v", err)
	}

	data := NewTemplateData(&Presentation{
		ContactEmail:  "rip@devco.net",
		ContactSocial: "@ripienaar@devco.social",
	})
	data.Slide = &Slide{Number: 1, PageStyle: "closing"}
	data.Heading = "Questions?"
	data.CTA = "choria-cm.dev"

	out, err := theme.Render("closing", data)
	if err != nil {
		t.Fatalf("closing did not render: %v", err)
	}

	for _, want := range []string{"=rip@devco.net", "=@ripienaar@devco.social", `class="cta">choria-cm.dev`} {
		if !strings.Contains(out, want) {
			t.Errorf("the rendered closing slide does not carry %q, got:\n%s", want, out)
		}
	}
}

func TestDanglingIncludeFailsAtLoad(t *testing.T) {
	// Jet resolves an include when the template runs, so a page style naming a
	// partial that is not there used to load clean and fail on the slide that
	// first reached it, which for a page style is mid-talk.
	dir := writeTheme(t, map[string]string{
		"theme.yaml":         "code_style: bw\n",
		"styles/content.jet": "<p>{{ raw: body }}</p>\n{{ include \"_missing.jet\" }}\n",
	})

	_, err := LoadTheme(dir, t.TempDir())
	if err == nil {
		t.Fatal("a theme including a partial that does not exist loaded")
	}

	if !errors.Is(err, ErrThemeTemplate) {
		t.Fatalf("error was %v, want ErrThemeTemplate", err)
	}

	if !strings.Contains(err.Error(), "_missing.jet") {
		t.Errorf("the error does not name the missing partial: %v", err)
	}
}

func TestThemeWithoutStylesheetFailsAtLoad(t *testing.T) {
	dir := writeTheme(t, map[string]string{
		"theme.yaml":         "code_style: bw\n",
		"theme.css":          "",
		"styles/content.jet": "<p>{{ raw: body }}</p>\n",
	})

	err := os.Remove(filepath.Join(dir, "theme.css"))
	if err != nil {
		t.Fatalf("cannot remove the stylesheet: %v", err)
	}

	_, err = LoadTheme(dir, t.TempDir())
	if err == nil {
		t.Fatal("a theme with no theme.css loaded, so every slide would render unstyled")
	}

	if !errors.Is(err, ErrThemeConfig) {
		t.Fatalf("error was %v, want ErrThemeConfig", err)
	}
}
