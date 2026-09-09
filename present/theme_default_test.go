package present

import (
	"io/fs"
	"strings"
	"testing"
)

// defaultShow is a presentation with every field the theme places, so a test
// that a slot went missing fails rather than rendering an empty deck.
//
// The handle and the addresses are a person's, which is what a deck carries.
// The theme is the one that ships in the binary.
func defaultShow() *Presentation {
	return &Presentation{
		Title:    "Choria Config Manager",
		Subtitle: "Configuration management over NATS",
		Event:    "Configuration Management Camp 2026",
		Date:     "2026-02-02",
		Theme:    "default",
		Presenter: Presenter{
			Name:    "R.I.",
			Surname: "Pienaar",
		},
		ContactEmail:  "rip@devco.net",
		ContactWeb:    "https://devco.net",
		ContactSocial: "@ripienaar@devco.social",
	}
}

func loadDefaultTheme(t *testing.T) *Theme {
	t.Helper()

	theme, err := LoadTheme("default", t.TempDir())
	if err != nil {
		t.Fatalf("cannot load the default theme: %v", err)
	}

	return theme
}

func TestDefaultThemeStyles(t *testing.T) {
	theme := loadDefaultTheme(t)

	// The seven styles a deck writes page_style with. A missing one is a slide
	// nobody can build.
	want := []string{"closing", "code", "columns", "content", "image", "section", "title"}

	got := theme.Styles()
	if len(got) != len(want) {
		t.Fatalf("styles are %v, want %v", got, want)
	}

	for i, style := range want {
		if got[i] != style {
			t.Fatalf("styles are %v, want %v", got, want)
		}
	}

	if !theme.Splits("columns") {
		t.Error("columns does not split, so a two column slide renders as one")
	}

	if theme.Splits("content") {
		t.Error("content splits, so a thematic break in prose would cut the slide in half")
	}
}

func TestDefaultThemeFooterOnEveryStyleButTitle(t *testing.T) {
	theme := loadDefaultTheme(t)
	show := defaultShow()

	for _, style := range theme.Styles() {
		data := NewTemplateData(show)
		data.Slide = &Slide{Number: 1, PageStyle: style}
		data.Heading = "A heading"
		data.Body = "<p>A body</p>"
		data.Left = "<p>Left</p>"
		data.Right = "<p>Right</p>"

		out, err := theme.Render(style, data)
		if err != nil {
			t.Fatalf("%s did not render: %v", style, err)
		}

		hasFooter := strings.Contains(out, "slide-footer")

		if style == "title" && hasFooter {
			t.Error("the title slide carries a footer")
		}

		if style != "title" && !hasFooter {
			t.Errorf("%s carries no footer", style)
		}
	}
}

func TestDefaultThemeFooterParts(t *testing.T) {
	theme := loadDefaultTheme(t)

	data := NewTemplateData(defaultShow())
	data.Slide = &Slide{Number: 2, PageStyle: "section"}
	data.Heading = "Antifragility"

	out, err := theme.Render("section", data)
	if err != nil {
		t.Fatalf("section did not render: %v", err)
	}

	// Name, email, web and social, in the order the footer reads them.
	for _, part := range []string{"R.I. Pienaar", "rip@devco.net", "https://devco.net", "@ripienaar@devco.social"} {
		if !strings.Contains(out, part) {
			t.Errorf("the footer does not carry %q", part)
		}
	}
}

func TestDefaultThemeTitlePlacesEverySlot(t *testing.T) {
	theme := loadDefaultTheme(t)

	show := defaultShow()
	show.Presenter.Avatar = "avatar.png"

	data := NewTemplateData(show)
	data.Slide = &Slide{Number: 1, PageStyle: "title"}
	data.Body = `<p><img src="logo.png" alt=""></p>`

	out, err := theme.Render("title", data)
	if err != nil {
		t.Fatalf("title did not render: %v", err)
	}

	// The subtitle and the avatar had no slot when the theme was first drawn, so
	// each is asserted rather than left to a reader of the CSS.
	for _, want := range []string{"logo.png", "Configuration management over NATS", "Configuration Management Camp 2026", "avatar.png", "R.I. Pienaar"} {
		if !strings.Contains(out, want) {
			t.Errorf("the title slide does not carry %q", want)
		}
	}
}

func TestDefaultThemeTitleFallsBackToThePresentationTitle(t *testing.T) {
	theme := loadDefaultTheme(t)

	data := NewTemplateData(defaultShow())
	data.Slide = &Slide{Number: 1, PageStyle: "title"}

	out, err := theme.Render("title", data)
	if err != nil {
		t.Fatalf("title did not render: %v", err)
	}

	if !strings.Contains(out, "Choria Config Manager") {
		t.Error("a title slide with no logo shows no title")
	}
}

func TestDefaultThemeFooterOverride(t *testing.T) {
	theme := loadDefaultTheme(t)

	show := defaultShow()
	show.Footer = "Internal, do not share"

	data := NewTemplateData(show)
	data.Slide = &Slide{Number: 3, PageStyle: "content"}
	data.Body = "<p>Body</p>"

	out, err := theme.Render("content", data)
	if err != nil {
		t.Fatalf("content did not render: %v", err)
	}

	if !strings.Contains(out, "Internal, do not share") {
		t.Error("the footer string was not placed")
	}

	if strings.Contains(out, "rip@devco.net") {
		t.Error("the derived footer still shows beside the footer string that replaces it")
	}
}

func TestDefaultThemeClosingListsContacts(t *testing.T) {
	theme := loadDefaultTheme(t)

	show := defaultShow()
	show.Presenter.GitHub = "ripienaar"

	data := NewTemplateData(show)
	data.Slide = &Slide{Number: 9, PageStyle: "closing"}
	data.Heading = "Questions?"
	data.CTA = "choria-cm.dev"

	out, err := theme.Render("closing", data)
	if err != nil {
		t.Fatalf("closing did not render: %v", err)
	}

	for _, want := range []string{"Questions?", "@ripienaar@devco.social", "rip@devco.net", "https://devco.net", "ripienaar", "choria-cm.dev"} {
		if !strings.Contains(out, want) {
			t.Errorf("the closing slide does not carry %q", want)
		}
	}
}

func TestDefaultThemeColumnsPlacesBothHalves(t *testing.T) {
	theme := loadDefaultTheme(t)

	data := NewTemplateData(defaultShow())
	data.Slide = &Slide{Number: 4, PageStyle: "columns"}
	data.Heading = "Project Status"
	data.Left = "<ul><li>Two months old</li></ul>"
	data.Right = "<ul><li>Better facts</li></ul>"
	data.CTA = "choria-cm.dev"

	out, err := theme.Render("columns", data)
	if err != nil {
		t.Fatalf("columns did not render: %v", err)
	}

	if !strings.Contains(out, "Two months old") || !strings.Contains(out, "Better facts") {
		t.Error("a column half is missing")
	}

	if strings.Index(out, "Two months old") > strings.Index(out, "Better facts") {
		t.Error("the halves are the wrong way round")
	}
}

func TestDefaultThemeEscapesFrontmatterAndNotTheBody(t *testing.T) {
	theme := loadDefaultTheme(t)

	show := defaultShow()
	show.Subtitle = "Fish & Chips"

	data := NewTemplateData(show)
	data.Slide = &Slide{Number: 1, PageStyle: "title"}
	data.Body = "<p><em>markup</em></p>"

	out, err := theme.Render("title", data)
	if err != nil {
		t.Fatalf("title did not render: %v", err)
	}

	// The templates are the only thing that decides this, so the theme asserts it
	// rather than trusting the renderer to hand it safe values.
	if !strings.Contains(out, "Fish &amp; Chips") {
		t.Error("a value from presentation.yaml was written unescaped")
	}

	if !strings.Contains(out, "<em>markup</em>") {
		t.Error("the rendered body was escaped rather than written raw")
	}
}

// TestDefaultThemeShipsItsFonts pins that the theme carries its own font files,
// so a deck looks the same on a machine that has never heard of IBM Plex and an
// exported file carries them inline. A stylesheet naming a face the theme does
// not ship would fall back to whatever the browser had.
func TestDefaultThemeShipsItsFonts(t *testing.T) {
	theme := loadDefaultTheme(t)

	css, err := fs.ReadFile(theme.FS(), "theme.css")
	if err != nil {
		t.Fatalf("cannot read the stylesheet: %v", err)
	}

	// A url() the deck directory cannot answer for is not a file the theme has to
	// ship: the checkbox tick is a data URI, which carries its own content and is
	// dropped by the exporter for the same reason.
	var named []string

	for _, match := range cssURLRe.FindAllStringSubmatch(string(css), -1) {
		ref, ok := relativeAsset(refValue(match))
		if !ok {
			continue
		}

		named = append(named, ref)
	}

	if len(named) == 0 {
		t.Fatal("the stylesheet names no font file")
	}

	for _, ref := range named {
		_, err := fs.Stat(theme.FS(), ref)
		if err != nil {
			t.Errorf("the stylesheet names %q, which the theme does not ship: %v", ref, err)
		}
	}

	entries, err := fs.ReadDir(theme.FS(), "fonts")
	if err != nil {
		t.Fatalf("the theme ships no fonts directory: %v", err)
	}

	if len(entries) != len(named) {
		t.Errorf("the theme ships %d font files and names %d", len(entries), len(named))
	}
}

// TestDefaultThemeRendersEveryStyle renders a slide through each style, since a
// template that only fails when a deck happens to use it fails during a talk.
func TestDefaultThemeRendersEveryStyle(t *testing.T) {
	theme := loadDefaultTheme(t)

	show := defaultShow()
	show.Width = 1280
	show.Height = 720

	for _, style := range theme.Styles() {
		data := NewTemplateData(show)
		data.Slide = &Slide{Number: 3, PageStyle: style}
		data.Heading = "A heading"
		data.Body = "<p>A body</p>"
		data.Left = "<p>Left</p>"
		data.Right = "<p>Right</p>"
		data.Caption = "A caption"
		data.CTA = "A call to action"

		out, err := theme.Render(style, data)
		if err != nil {
			t.Fatalf("%s did not render: %v", style, err)
		}

		hasFooter := strings.Contains(out, "slide-footer")

		if style == "title" && hasFooter {
			t.Error("the title slide carries a footer")
		}

		if style != "title" && !hasFooter {
			t.Errorf("%s carries no footer", style)
		}

		// The theme numbers its slides in the footer, in the same mono the code
		// slide is set in.
		if style != "title" && !strings.Contains(out, ">3<") {
			t.Errorf("%s does not carry the slide number", style)
		}
	}
}
