package present

import (
	"os"
	"strings"
	"testing"
)

// decodeIn reads data back the way the editor does, through a root on a
// directory of the test's own.
func decodeIn(t *testing.T, data []byte) *Deck {
	t.Helper()

	dir := t.TempDir()

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("cannot open the deck root: %v", err)
	}

	deck := DecodeDeck(dir, root, data)
	t.Cleanup(func() { deck.Close() })

	return deck
}

func TestEncodeDeckRoundTrip(t *testing.T) {
	presentation := Presentation{
		Title:                "Writing Slides in Markdown",
		Subtitle:             "A deck is one file",
		Event:                "An Example Conference",
		Date:                 "2026-02-02",
		Theme:                "default",
		Aspect:               "16:9",
		Transition:           "fade",
		Presenter:            Presenter{Name: "Alex", Surname: "Rivera", Avatar: "images/avatar.svg"},
		ContactEmail:         "alex@example.net",
		ContactSocial:        "@alex@example.social",
		ContactSocialNetwork: "Mastodon",
	}

	slides := []*Slide{
		{PageStyle: "title", Markdown: "![](images/logo.svg)"},
		{
			PageStyle: "content",
			Caption:   "One file, the order in it is the order of the talk",
			CTA:       "presentmd edit ./deck",
			TextSize:  "small",
			Notes:     "The first line of the notes.\n\nAnd a second paragraph under it.",
			Markdown:  "# A Heading\n\nThe body, as **markdown**.",
		},
		{PageStyle: "section", Background: "#dce8e6", Markdown: "# A Divider"},
		{PageStyle: "closing"},
	}

	data, err := EncodeDeck(presentation, slides)
	if err != nil {
		t.Fatalf("cannot encode the deck: %v", err)
	}

	deck := decodeIn(t, data)

	if len(deck.Problems) != 0 {
		t.Fatalf("the deck came back with problems: %v", deck.Problems)
	}

	if deck.Presentation.Title != presentation.Title || deck.Presentation.Theme != presentation.Theme {
		t.Fatalf("the presentation did not survive: %+v", deck.Presentation)
	}

	if deck.Presentation.Presenter != presentation.Presenter {
		t.Fatalf("the presenter did not survive: %+v", deck.Presentation.Presenter)
	}

	if deck.Presentation.ContactSocialNetwork != presentation.ContactSocialNetwork {
		t.Fatalf("the contacts did not survive: %+v", deck.Presentation)
	}

	if len(deck.Slides) != len(slides) {
		t.Fatalf("read back %d slides, wrote %d:\n%s", len(deck.Slides), len(slides), data)
	}

	for i, want := range slides {
		got := deck.Slides[i]

		if got.PageStyle != want.PageStyle || got.Caption != want.Caption || got.CTA != want.CTA ||
			got.TextSize != want.TextSize || got.Background != want.Background {
			t.Fatalf("slide %d did not survive: %+v", i+1, got)
		}

		if got.Notes != want.Notes {
			t.Fatalf("slide %d notes did not survive: %q", i+1, got.Notes)
		}

		if got.Markdown != strings.TrimSpace(want.Markdown) {
			t.Fatalf("slide %d body did not survive: %q", i+1, got.Markdown)
		}

		if got.Number != i+1 {
			t.Fatalf("slide %d came back as number %d", i+1, got.Number)
		}
	}
}

// The notes of a slide are prose someone also opens in a text editor, so they
// are written as a block rather than as one escaped line.
func TestEncodeDeckWritesNotesAsABlock(t *testing.T) {
	data, err := EncodeDeck(Presentation{Theme: "default"}, []*Slide{
		{PageStyle: "content", Notes: "One line.\nAnd another.", Markdown: "# A Heading"},
	})
	if err != nil {
		t.Fatalf("cannot encode the deck: %v", err)
	}

	if !strings.Contains(string(data), "notes: |") {
		t.Fatalf("the notes were not written as a block:\n%s", data)
	}
}

func TestEncodeDeckRefusesABreakInABody(t *testing.T) {
	_, err := EncodeDeck(Presentation{Theme: "default"}, []*Slide{
		{PageStyle: "content", Markdown: "# A Heading\n\n+++\n\nand more"},
	})
	if err == nil {
		t.Fatal("a body carrying a slide break was encoded")
	}

	if !strings.Contains(err.Error(), "slide 1") {
		t.Fatalf("the refusal does not name the slide: %v", err)
	}
}

// A break inside a fenced code block is the deck's own text, which is what lets
// a talk about presentmd show a presentation.md on a slide.
func TestEncodeDeckKeepsABreakInAFence(t *testing.T) {
	body := "# What a Slide Looks Like\n\n```markdown\n+++\npage_style: content\n+++\n```"

	data, err := EncodeDeck(Presentation{Theme: "default"}, []*Slide{{PageStyle: "code", Markdown: body}})
	if err != nil {
		t.Fatalf("cannot encode the deck: %v", err)
	}

	deck := decodeIn(t, data)

	if len(deck.Slides) != 1 {
		t.Fatalf("read back %d slides, wrote 1:\n%s", len(deck.Slides), data)
	}

	if deck.Slides[0].Markdown != body {
		t.Fatalf("the fenced break did not survive: %q", deck.Slides[0].Markdown)
	}
}

// An empty deck is what New deck leaves behind: a presentation and no slides.
func TestEncodeDeckWithNoSlides(t *testing.T) {
	data, err := EncodeDeck(Presentation{Title: "A New Talk", Theme: "default"}, nil)
	if err != nil {
		t.Fatalf("cannot encode the deck: %v", err)
	}

	deck := decodeIn(t, data)

	if len(deck.Slides) != 0 {
		t.Fatalf("read back %d slides, wrote none:\n%s", len(deck.Slides), data)
	}

	if deck.Presentation.Title != "A New Talk" {
		t.Fatalf("the presentation did not survive: %+v", deck.Presentation)
	}
}
