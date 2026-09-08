package present

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// exportDeck writes a deck of the named files in a temp directory and loads it.
// An export test builds its own deck because what an export has to carry is the
// files a deck names, and the fixture deck ships none of the images its
// presentation.yaml names.
func exportDeck(t *testing.T, files map[string]string) *Deck {
	t.Helper()

	dir := t.TempDir()

	for name, body := range files {
		at := filepath.Join(dir, filepath.FromSlash(name))

		err := os.MkdirAll(filepath.Dir(at), 0o700)
		if err != nil {
			t.Fatalf("cannot make the directory for %s: %v", name, err)
		}

		writeFile(t, at, body)
	}

	deck, err := LoadDeck(dir)
	if err != nil {
		t.Fatalf("cannot load the deck that was written: %v", err)
	}

	t.Cleanup(func() {
		deck.Close()
	})

	return deck
}

// exported is the page, and fails the test for an export that would not write
// one.
func exported(t *testing.T, deck *Deck, theme *Theme, opts exportOptions) (string, []Problem) {
	t.Helper()

	page, problems, err := export(deck, theme, opts)
	if err != nil {
		t.Fatalf("the deck did not export: %v, problems %v", err, problems)
	}

	if len(page) == 0 {
		t.Fatal("the export wrote no page")
	}

	return string(page), problems
}

// scriptBodyRe is the text of a script element. The reference forms are read
// out of the finished page, and the script reveal ships holds `src=` and
// `href=` inside its own strings, which are the browser's to run rather than
// anything the page fetches.
var scriptBodyRe = regexp.MustCompile(`(?s)(<script[^>]*>).*?(</script>)`)

// unresolved is every reference the finished page makes that a browser would
// have to fetch from beside the file. A data URI is carried in the page, a
// scheme is a link the author wrote to the web, and a fragment points inside
// the page itself, so what is left is a relative path, which is what an export
// is meant to leave none of.
func unresolved(page string) []string {
	page = scriptBodyRe.ReplaceAllString(page, "$1$2")

	var refs []string

	for _, match := range assetRefRe.FindAllStringSubmatch(page, -1) {
		refs = append(refs, refValue(match))
	}

	for _, match := range cssURLRe.FindAllStringSubmatch(page, -1) {
		refs = append(refs, refValue(match))
	}

	var left []string

	for _, ref := range refs {
		ref = strings.TrimSpace(ref)

		switch {
		case ref == "", strings.HasPrefix(ref, "#"):
		case schemeRe.MatchString(ref):
		default:
			left = append(left, ref)
		}
	}

	return left
}

func pngURI(content string) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte(content))
}

// TestExportInlinesEveryReference is the whole of what the design asks for: the
// file opens from file:// with nothing left to fetch, and the link a slide's
// prose wrote to the web is still the author's.
func TestExportInlinesEveryReference(t *testing.T) {
	deck := exportDeck(t, map[string]string{
		"presentation.yaml": "title: A Talk\ntheme: default\nfooter: planboard\n" +
			"presenter:\n  name: R.I.\n  surname: Pienaar\n  avatar: images/avatar.png\n",
		"01-title.md": "---\npage_style: title\n---\n\n# A Talk\n",
		"02-content.md": "---\npage_style: content\nbackground: images/backdrop.png\n---\n\n" +
			"# What it is\n\n" +
			"![rendered](images/goldmark.png)\n\n" +
			"<img src=\"images/hand.png\" alt=\"hand written\">\n\n" +
			"[elsewhere](https://example.net/away)\n",
		"images/avatar.png":   "avatar bytes",
		"images/backdrop.png": "backdrop bytes",
		"images/goldmark.png": "goldmark bytes",
		"images/hand.png":     "hand bytes",
	})

	page, problems := exported(t, deck, loadDefaultTheme(t), exportOptions{})

	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}

	left := unresolved(page)
	if len(left) != 0 {
		t.Errorf("the file still points at %v", left)
	}

	for _, want := range []string{
		`data-background-image="` + pngURI("backdrop bytes") + `"`,
		`src="` + pngURI("goldmark bytes") + `"`,
		`src="` + pngURI("hand bytes") + `"`,
		`src="` + pngURI("avatar bytes") + `"`,
		`href="https://example.net/away"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the file does not hold %q", want)
		}
	}

	// Reveal and the theme are carried in the page rather than beside it, and
	// reveal.js is written out where the notes plugin is a data URI, since the
	// plugin's own text would end the element it was written into.
	for _, gone := range []string{`href="reveal/`, `src="reveal/`, `href="theme/`, `src="assets/`} {
		if strings.Contains(page, gone) {
			t.Errorf("the file still names %q", gone)
		}
	}

	if !strings.Contains(page, "Reveal.initialize") || !strings.Contains(page, "RevealNotes") {
		t.Error("the page lost the script that starts reveal")
	}
}

// TestExportCarriesTheFixtureDeck runs the deck and the theme the rest of the
// package tests against, and pins the other half of the rule: a file the deck
// names and the exporter cannot read is reported and the file is still written,
// naming the path it lacks.
func TestExportCarriesTheFixtureDeck(t *testing.T) {
	deck, err := LoadDeck(deckDir)
	if err != nil {
		t.Fatalf("cannot load the deck fixture: %v", err)
	}
	defer deck.Close()

	page, problems := exported(t, deck, loadDefaultTheme(t), exportOptions{})

	// The fixture names an avatar it does not ship, which is the unreadable file
	// this pins. Anything else is a problem this test wants to hear about.
	if len(problems) != 1 || problems[0].Path != "images/avatar.png" {
		t.Fatalf("problems %v, want the avatar the fixture does not ship", problems)
	}

	if !strings.Contains(page, "assets/images/avatar.png") {
		t.Error("the file dropped the path it could not carry rather than naming it")
	}

	if unresolved(page) == nil {
		t.Error("the avatar that was not carried left no reference behind")
	}

	for _, gone := range []string{`href="reveal/`, `src="reveal/`, `href="theme/`} {
		if strings.Contains(page, gone) {
			t.Errorf("the file still names %q", gone)
		}
	}

	// Chroma's stylesheet is written into the page by the render and stays there.
	if !strings.Contains(page, ".chroma") {
		t.Error("the page holds no code stylesheet")
	}
}

// TestExportInlinesAThemeFont pins the second asset list: a font the theme's
// stylesheet names is read through the theme rather than through the deck, and
// lands in the stylesheet the page carries.
func TestExportInlinesAThemeFont(t *testing.T) {
	themeDir := t.TempDir()

	err := os.CopyFS(themeDir, os.DirFS(filepath.Join("testdata", "theme-fixture")))
	if err != nil {
		t.Fatalf("cannot copy the fixture theme: %v", err)
	}

	err = os.MkdirAll(filepath.Join(themeDir, "fonts"), 0o700)
	if err != nil {
		t.Fatalf("cannot make the theme's font directory: %v", err)
	}

	writeFile(t, filepath.Join(themeDir, "fonts", "fixture-sans.woff2"), "font bytes")

	deck := exportDeck(t, map[string]string{
		"presentation.yaml": "title: A Talk\ntheme: " + themeDir + "\n",
		"01-content.md":     "---\npage_style: content\n---\n\n# A Talk\n",
	})

	theme, err := LoadTheme(deck.Presentation.Theme, deck.Dir)
	if err != nil {
		t.Fatalf("cannot load the theme the deck names: %v", err)
	}

	page, problems := exported(t, deck, theme, exportOptions{})

	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}

	want := "data:font/woff2;base64," + base64.StdEncoding.EncodeToString([]byte("font bytes"))
	if !strings.Contains(page, want) {
		t.Error("the theme's font is not in the file")
	}

	if strings.Contains(page, "fonts/fixture-sans.woff2") {
		t.Error("the stylesheet still points at the font file")
	}
}

// TestExportReportsAVideoItCannotCarry pins the design's other reported case: a
// video is tens of megabytes and base64 makes a file no browser opens, so it is
// left as its path and the file is written.
func TestExportReportsAVideoItCannotCarry(t *testing.T) {
	deck := exportDeck(t, map[string]string{
		"presentation.yaml": "title: A Talk\ntheme: default\n",
		"01-content.md":     "---\npage_style: content\nbackground: media/loop.mp4\n---\n\n# A Talk\n",
		"media/loop.mp4":    "video bytes",
	})

	page, problems := exported(t, deck, loadDefaultTheme(t), exportOptions{})

	if len(problems) != 1 {
		t.Fatalf("problems %v, want the video alone", problems)
	}

	if problems[0].Path != "media/loop.mp4" || !strings.Contains(problems[0].Message, "video/mp4") {
		t.Errorf("the problem is %v, want it against the video and naming its type", problems[0])
	}

	if !strings.Contains(page, `data-background-image="assets/media/loop.mp4"`) {
		t.Error("the file does not name the video it could not carry")
	}
}

// TestExportFetchesTheAvatar pins the one fetch planboard makes: a github handle
// with no avatar file beside it is fetched here, and nowhere else, so the file
// opens offline. A fetch that fails leaves the slot empty and is reported, and
// the file is still written.
func TestExportFetchesTheAvatar(t *testing.T) {
	deck := exportDeck(t, map[string]string{
		"presentation.yaml": "title: A Talk\ntheme: default\npresenter:\n  name: R.I.\n  github: ripienaar\n",
		"01-title.md":       "---\npage_style: title\n---\n\n# A Talk\n",
	})

	theme := loadDefaultTheme(t)

	t.Run("carried into the page", func(t *testing.T) {
		var asked []string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			asked = append(asked, r.URL.Path)

			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("avatar bytes"))
		}))
		defer server.Close()

		page, problems := exported(t, deck, theme, exportOptions{client: server.Client(), host: server.URL + "/"})

		if len(problems) != 0 {
			t.Fatalf("problems %v", problems)
		}

		if len(asked) != 1 || asked[0] != "/ripienaar.png" {
			t.Fatalf("the export asked for %v, want the handle's avatar once", asked)
		}

		if !strings.Contains(page, pngURI("avatar bytes")) {
			t.Error("the avatar is not in the file")
		}

		if strings.Contains(page, "github.com/ripienaar.png") {
			t.Error("the file still points a browser at github")
		}
	})

	t.Run("left empty when the fetch fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "no", http.StatusInternalServerError)
		}))
		defer server.Close()

		page, problems := exported(t, deck, theme, exportOptions{client: server.Client(), host: server.URL + "/"})

		if len(problems) != 1 || problems[0].Path != presentationFile {
			t.Fatalf("problems %v, want the fetch alone against presentation.yaml", problems)
		}

		if !strings.Contains(problems[0].Message, "500") {
			t.Errorf("the problem is %q and does not say what the fetch answered", problems[0].Message)
		}

		if strings.Contains(page, `<div class="title-avatar">`) {
			t.Error("the title slide holds an avatar the fetch never got")
		}

		if len(unresolved(page)) != 0 {
			t.Errorf("the file points at %v", unresolved(page))
		}
	})
}

// TestExportRefusesADeckWithProblems pins that the load's problems and the
// render's stop the write: a slide the theme has no page style for is missing
// from the page, and a talk with a slide missing is not one to hand to anyone.
func TestExportRefusesADeckWithProblems(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "a page style the theme does not have",
			files: map[string]string{
				"presentation.yaml": "title: A Talk\ntheme: default\n",
				"01-content.md":     "---\npage_style: nosuchstyle\n---\n\n# A Talk\n",
			},
		},
		{
			name: "a key presentation.yaml does not have",
			files: map[string]string{
				"presentation.yaml": "title: A Talk\ntheme: default\ncontact_socail: rip@devco.net\n",
				"01-content.md":     "---\npage_style: content\n---\n\n# A Talk\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deck := exportDeck(t, tc.files)

			page, problems, err := export(deck, loadDefaultTheme(t), exportOptions{})
			if !errors.Is(err, ErrDeckProblems) {
				t.Fatalf("the export answered %v, want one wrapping ErrDeckProblems", err)
			}

			if page != nil {
				t.Error("a deck with problems was written anyway")
			}

			if len(problems) == 0 {
				t.Error("the export refused the deck and said nothing about why")
			}
		})
	}
}

// initializeCall is the Reveal.initialize call a page carries, which is what
// decides how the deck moves.
func initializeCall(t *testing.T, page string) string {
	t.Helper()

	start := strings.Index(page, "Reveal.initialize({")
	if start < 0 {
		t.Fatal("the page does not initialize reveal")
	}

	end := strings.Index(page[start:], "});")
	if end < 0 {
		t.Fatal("the initialize call was opened and never closed")
	}

	return page[start : start+end]
}

// TestExportCarriesTheServedTransition pins that a deck exported to one file
// moves the way it moved live: the inlining replaces the stylesheets and the
// scripts and leaves what the page tells reveal alone.
func TestExportCarriesTheServedTransition(t *testing.T) {
	deck := exportDeck(t, map[string]string{
		"presentation.yaml": "title: A Talk\ntheme: default\ntransition: fade\ntransition_speed: fast\n",
		"01-title.md":       "---\npage_style: title\n---\n\n# A Talk\n",
		"02-content.md":     "---\npage_style: content\ntransition: zoom\n---\n\n# What it is\n",
	})

	theme := loadDefaultTheme(t)

	served, err := RenderDeck(deck, theme, RenderOptions{})
	if err != nil {
		t.Fatalf("the deck did not render: %v", err)
	}

	page, problems := exported(t, deck, theme, exportOptions{})

	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}

	if initializeCall(t, page) != initializeCall(t, served.HTML) {
		t.Errorf("the exported file initializes reveal with %q, served %q", initializeCall(t, page), initializeCall(t, served.HTML))
	}

	for _, want := range []string{`transition: "fade"`, `transitionSpeed: "fast"`, ` data-transition="zoom"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the exported file does not carry %q", want)
		}
	}
}

// TestExportReportsAThemeImport pins that a stylesheet the export does not
// follow is named rather than left as a relative reference in a file whose whole
// promise is that it opens with no network.
func TestExportReportsAThemeImport(t *testing.T) {
	dir := t.TempDir()

	for name, body := range map[string]string{
		"theme.yaml":         "code_style: bw\n",
		"theme.css":          "@import \"more.css\";\n.reveal { color: black; }\n",
		"more.css":           ".reveal h1 { color: navy; }\n",
		"styles/content.jet": "<div>{{ raw: body }}</div>\n",
	} {
		full := filepath.Join(dir, filepath.FromSlash(name))

		err := os.MkdirAll(filepath.Dir(full), 0o755)
		if err != nil {
			t.Fatalf("cannot create %s: %v", filepath.Dir(full), err)
		}

		err = os.WriteFile(full, []byte(body), 0o644)
		if err != nil {
			t.Fatalf("cannot write %s: %v", name, err)
		}
	}

	theme, err := LoadTheme(dir, t.TempDir())
	if err != nil {
		t.Fatalf("the theme did not load: %v", err)
	}

	deck := &Deck{
		Dir:          "testdata",
		Presentation: Presentation{Title: "Imports", Theme: dir, Width: 1280, Height: 720},
		Slides:       []*Slide{{Number: 1, Path: "01.md", PageStyle: "content", Markdown: "A body.\n"}},
	}

	_, problems, err := Export(deck, theme)
	if err != nil {
		t.Fatalf("the export failed: %v", err)
	}

	found := false

	for _, problem := range problems {
		if strings.Contains(problem.Message, "@import") {
			found = true
		}
	}

	if !found {
		t.Errorf("the import was not reported, problems are %v", problems)
	}
}
