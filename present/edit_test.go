package present

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// editorDeck is the deck an editor test opens on.
const editorDeck = `---
title: A Talk About Things
theme: default
---

+++
page_style: title
+++

+++
page_style: content
+++

# What They Are For

The body, as markdown.
`

// editorOn builds an editor on a root holding one deck in talk/, and returns it
// with the root directory.
func editorOn(t *testing.T) (*Editor, string) {
	t.Helper()

	rootDir := t.TempDir()

	err := os.Mkdir(filepath.Join(rootDir, "talk"), 0o755)
	if err != nil {
		t.Fatalf("cannot make the deck directory: %v", err)
	}

	err = os.WriteFile(filepath.Join(rootDir, "talk", presentationMarkdownFile), []byte(editorDeck), 0o644)
	if err != nil {
		t.Fatalf("cannot write the deck: %v", err)
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		t.Fatalf("cannot open the root: %v", err)
	}

	editor, err := NewEditor(EditorOptions{Log: handlerLogger(), Root: root, Deck: "talk"})
	if err != nil {
		root.Close()

		t.Fatalf("cannot build the editor: %v", err)
	}

	t.Cleanup(func() {
		editor.Close()
		root.Close()
	})

	return editor, rootDir
}

// ask makes one request of the editor as the page it served would: on a loopback
// host and carrying the token.
func ask(t *testing.T, editor *Editor, method string, target string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("cannot write the request: %v", err)
		}

		reader = bytes.NewReader(data)
	}

	req := httptest.NewRequest(method, target, reader)
	req.Host = "127.0.0.1:8080"
	req.Header.Set(tokenHeader, editor.token)

	rec := httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()

	err := json.Unmarshal(rec.Body.Bytes(), into)
	if err != nil {
		t.Fatalf("cannot read the response: %v: %s", err, rec.Body.String())
	}
}

func TestEditorServesThePage(t *testing.T) {
	editor, _ := editorOn(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8080"

	rec := httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the page answered %d", rec.Code)
	}

	if strings.Contains(rec.Body.String(), sessionMark) {
		t.Fatal("the page still carries the session placeholder")
	}

	if !strings.Contains(rec.Body.String(), editor.token) {
		t.Fatal("the page carries no token")
	}
}

// The page and the deck under it are the same server, so the deck the editor is
// on renders under the preview prefix.
func TestEditorServesThePreview(t *testing.T) {
	editor, _ := editorOn(t)

	req := httptest.NewRequest(http.MethodGet, previewPrefix+"/", nil)
	req.Host = "127.0.0.1:8080"

	rec := httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the preview answered %d", rec.Code)
	}

	if !strings.Contains(rec.Body.String(), "What They Are For") {
		t.Fatal("the preview does not carry the deck")
	}
}

func TestEditorOpensADeck(t *testing.T) {
	editor, _ := editorOn(t)

	rec := ask(t, editor, http.MethodGet, "/api/deck?path=talk", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("opening the deck answered %d: %s", rec.Code, rec.Body.String())
	}

	var payload deckPayload
	decodeBody(t, rec, &payload)

	if payload.Presentation.Title != "A Talk About Things" {
		t.Fatalf("the presentation did not come back: %+v", payload.Presentation)
	}

	if len(payload.Slides) != 2 {
		t.Fatalf("the deck came back with %d slides", len(payload.Slides))
	}

	if payload.Revision == 0 {
		t.Fatal("a deck on disk came back with no revision")
	}
}

func TestEditorSaveWritesTheDeck(t *testing.T) {
	editor, rootDir := editorOn(t)

	rec := ask(t, editor, http.MethodGet, "/api/deck?path=talk", nil)

	var payload deckPayload
	decodeBody(t, rec, &payload)

	payload.Presentation.Subtitle = "And what they are for"
	payload.Slides[1].Caption = "a caption the editor added"

	rec = ask(t, editor, http.MethodPost, "/api/save", deckRequest{
		Path:         "talk",
		Presentation: payload.Presentation,
		Slides:       payload.Slides,
		Revision:     payload.Revision,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("the save answered %d: %s", rec.Code, rec.Body.String())
	}

	deck, err := LoadDeck(filepath.Join(rootDir, "talk"))
	if err != nil {
		t.Fatalf("cannot load the deck that was written: %v", err)
	}
	defer deck.Close()

	if len(deck.Problems) != 0 {
		t.Fatalf("the deck that was written has problems: %v", deck.Problems)
	}

	if deck.Presentation.Subtitle != "And what they are for" {
		t.Fatalf("the presentation was not written: %+v", deck.Presentation)
	}

	if len(deck.Slides) != 2 || deck.Slides[1].Caption != "a caption the editor added" {
		t.Fatalf("the slides were not written: %+v", deck.Slides)
	}
}

// The browser holds the deck as JSON and hands it back, and a modification time
// in nanoseconds is larger than the integer it holds exactly. The revision has
// to survive that round trip: carried as a number it came back rounded, and
// every save read as a file that had changed on disk.
func TestEditorSaveSurvivesTheBrowsersJSON(t *testing.T) {
	editor, _ := editorOn(t)

	rec := ask(t, editor, http.MethodGet, "/api/deck?path=talk", nil)

	// A map is what the browser holds: every number in it is a float64, the
	// double a browser has.
	var held map[string]any
	decodeBody(t, rec, &held)

	if _, ok := held["revision"].(string); !ok {
		t.Fatalf("the revision crossed as %T, which a browser cannot hold exactly", held["revision"])
	}

	body, err := json.Marshal(map[string]any{
		"path":         "talk",
		"presentation": held["presentation"],
		"slides":       held["slides"],
		"revision":     held["revision"],
	})
	if err != nil {
		t.Fatalf("cannot encode the deck the browser holds: %v", err)
	}

	rec = ask(t, editor, http.MethodPost, "/api/save", json.RawMessage(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("a save carrying the revision it opened answered %d: %s", rec.Code, rec.Body.String())
	}
}

// A save carries the revision it opened, so a file something else wrote in the
// meantime is reported rather than written over.
func TestEditorSaveRefusesAStaleRevision(t *testing.T) {
	editor, rootDir := editorOn(t)

	rec := ask(t, editor, http.MethodGet, "/api/deck?path=talk", nil)

	var payload deckPayload
	decodeBody(t, rec, &payload)

	rec = ask(t, editor, http.MethodPost, "/api/save", deckRequest{
		Path:         "talk",
		Presentation: payload.Presentation,
		Slides:       payload.Slides,
		Revision:     payload.Revision - 1,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("a stale save answered %d: %s", rec.Code, rec.Body.String())
	}

	rec = ask(t, editor, http.MethodPost, "/api/save", deckRequest{
		Path:         "talk",
		Presentation: payload.Presentation,
		Slides:       payload.Slides,
		Revision:     payload.Revision - 1,
		Force:        true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a forced save answered %d: %s", rec.Code, rec.Body.String())
	}

	_, err := os.Stat(filepath.Join(rootDir, "talk", draftName))
	if !os.IsNotExist(err) {
		t.Fatal("the file a save writes before it renames it was left behind")
	}
}

func TestEditorCreatesADeck(t *testing.T) {
	editor, rootDir := editorOn(t)

	rec := ask(t, editor, http.MethodPost, "/api/decks", deckRequest{
		Name:         "new-talk",
		Presentation: Presentation{Title: "new talk", Theme: "default"},
		Slides:       []*Slide{{PageStyle: "title"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("making a deck answered %d: %s", rec.Code, rec.Body.String())
	}

	deck, err := LoadDeck(filepath.Join(rootDir, "new-talk"))
	if err != nil {
		t.Fatalf("cannot load the deck that was made: %v", err)
	}
	defer deck.Close()

	if deck.Presentation.Title != "new talk" || len(deck.Slides) != 1 {
		t.Fatalf("the deck that was made is not the one that was asked for: %+v", deck.Presentation)
	}
}

// A new deck is one directory directly under the root, so a name that reads as a
// path is refused before it reaches the file system.
func TestEditorRefusesADeckNameThatIsAPath(t *testing.T) {
	editor, _ := editorOn(t)

	for _, name := range []string{"../escape", "a/b", "/absolute", "", "."} {
		rec := ask(t, editor, http.MethodPost, "/api/decks", deckRequest{
			Name:         name,
			Presentation: Presentation{Theme: "default"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("the name %q answered %d: %s", name, rec.Code, rec.Body.String())
		}
	}
}

// The root is the confinement. A path that climbs out of it is refused, and so
// is a symlink inside it that points out of it.
func TestEditorRefusesADeckOutsideTheRoot(t *testing.T) {
	editor, rootDir := editorOn(t)

	rec := ask(t, editor, http.MethodGet, "/api/deck?path=../elsewhere", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a path above the root answered %d: %s", rec.Code, rec.Body.String())
	}

	outside := t.TempDir()

	err := os.WriteFile(filepath.Join(outside, presentationMarkdownFile), []byte(editorDeck), 0o644)
	if err != nil {
		t.Fatalf("cannot write the deck outside the root: %v", err)
	}

	err = os.Symlink(outside, filepath.Join(rootDir, "linked"))
	if err != nil {
		t.Fatalf("cannot link out of the root: %v", err)
	}

	rec = ask(t, editor, http.MethodGet, "/api/deck?path=linked", nil)
	if rec.Code == http.StatusOK {
		t.Fatal("a symlink out of the root was followed")
	}

	rec = ask(t, editor, http.MethodPost, "/api/save", deckRequest{
		Path:         "linked",
		Presentation: Presentation{Theme: "default"},
	})
	if rec.Code == http.StatusOK {
		t.Fatal("a save through a symlink out of the root was written")
	}

	_, err = os.Stat(filepath.Join(outside, draftName))
	if !os.IsNotExist(err) {
		t.Fatal("a save reached a directory outside the root")
	}
}

func TestEditorRefusesARequestWithoutTheToken(t *testing.T) {
	editor, _ := editorOn(t)

	req := httptest.NewRequest(http.MethodPost, "/api/save", strings.NewReader(`{"path":"talk"}`))
	req.Host = "127.0.0.1:8080"

	rec := httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("a request with no token answered %d", rec.Code)
	}
}

func TestEditorRefusesAnotherOrigin(t *testing.T) {
	editor, _ := editorOn(t)

	req := httptest.NewRequest(http.MethodPost, "/api/save", strings.NewReader(`{"path":"talk"}`))
	req.Host = "127.0.0.1:8080"
	req.Header.Set(tokenHeader, editor.token)
	req.Header.Set("Origin", "http://deck.example.net")

	rec := httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("a request from another origin answered %d", rec.Code)
	}
}

// A name on the network that resolves to this machine reaches the editor with
// its own host, which is what the loopback check is for.
func TestEditorRefusesANonLoopbackHost(t *testing.T) {
	editor, _ := editorOn(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "deck.example.net"

	rec := httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("a request on another host answered %d", rec.Code)
	}
}

func TestEditorListsTheDecksUnderTheRoot(t *testing.T) {
	editor, rootDir := editorOn(t)

	err := os.MkdirAll(filepath.Join(rootDir, "conference", "keynote"), 0o755)
	if err != nil {
		t.Fatalf("cannot make a deck directory: %v", err)
	}

	err = os.WriteFile(filepath.Join(rootDir, "conference", "keynote", presentationMarkdownFile), []byte(editorDeck), 0o644)
	if err != nil {
		t.Fatalf("cannot write a deck: %v", err)
	}

	err = os.MkdirAll(filepath.Join(rootDir, ".git", "hidden"), 0o755)
	if err != nil {
		t.Fatalf("cannot make a hidden directory: %v", err)
	}

	err = os.WriteFile(filepath.Join(rootDir, ".git", "hidden", presentationMarkdownFile), []byte(editorDeck), 0o644)
	if err != nil {
		t.Fatalf("cannot write a deck: %v", err)
	}

	rec := ask(t, editor, http.MethodGet, "/api/decks", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("the listing answered %d: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		Root  string      `json:"root"`
		Decks []deckEntry `json:"decks"`
	}

	decodeBody(t, rec, &out)

	found := map[string]deckEntry{}
	for _, entry := range out.Decks {
		found[entry.Path] = entry
	}

	if _, ok := found["talk"]; !ok {
		t.Fatalf("the deck the editor is on was not listed: %+v", out.Decks)
	}

	if _, ok := found["conference/keynote"]; !ok {
		t.Fatalf("a deck under a directory was not listed: %+v", out.Decks)
	}

	if _, ok := found[".git/hidden"]; ok {
		t.Fatal("a deck under a hidden directory was listed")
	}

	if found["talk"].Title != "A Talk About Things" {
		t.Fatalf("a listed deck carries no title: %+v", found["talk"])
	}
}

// A draft renders without writing: the preview moves and the file does not.
func TestEditorDraftWritesNothing(t *testing.T) {
	editor, rootDir := editorOn(t)

	before, err := os.ReadFile(filepath.Join(rootDir, "talk", presentationMarkdownFile))
	if err != nil {
		t.Fatalf("cannot read the deck: %v", err)
	}

	rec := ask(t, editor, http.MethodPost, "/api/draft", deckRequest{
		Path:         "talk",
		Presentation: Presentation{Title: "A Draft", Theme: "default"},
		Slides:       []*Slide{{PageStyle: "content", Markdown: "# Being Typed"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("the draft answered %d: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		Markdown string    `json:"markdown"`
		Problems []Problem `json:"problems"`
	}

	decodeBody(t, rec, &out)

	if !strings.Contains(out.Markdown, "Being Typed") {
		t.Fatalf("the draft did not come back as a file: %s", out.Markdown)
	}

	after, err := os.ReadFile(filepath.Join(rootDir, "talk", presentationMarkdownFile))
	if err != nil {
		t.Fatalf("cannot read the deck: %v", err)
	}

	if !bytes.Equal(before, after) {
		t.Fatal("a draft wrote to the deck")
	}

	req := httptest.NewRequest(http.MethodGet, previewPrefix+"/", nil)
	req.Host = "127.0.0.1:8080"

	preview := httptest.NewRecorder()
	editor.ServeHTTP(preview, req)

	if !strings.Contains(preview.Body.String(), "Being Typed") {
		t.Fatal("the preview did not move onto the draft")
	}
}

// A deck the loader complains about is still served and the complaint reaches
// the browser, the way a broken deck under serve keeps presenting.
func TestEditorReportsProblems(t *testing.T) {
	editor, _ := editorOn(t)

	rec := ask(t, editor, http.MethodPost, "/api/draft", deckRequest{
		Path:         "talk",
		Presentation: Presentation{Title: "No Theme Here"},
		Slides:       []*Slide{{PageStyle: "content", Markdown: "# A Heading"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("the draft answered %d: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		Problems []Problem `json:"problems"`
	}

	decodeBody(t, rec, &out)

	if len(out.Problems) == 0 {
		t.Fatal("a deck naming no theme came back with no problems")
	}
}

// The editor writes one file. A deck written the other way is refused rather
// than opened as an empty talk that a save would leave two decks behind in.
func TestEditorRefusesADeckOfFilesPerSlide(t *testing.T) {
	editor, rootDir := editorOn(t)

	err := os.Mkdir(filepath.Join(rootDir, "old"), 0o755)
	if err != nil {
		t.Fatalf("cannot make the deck directory: %v", err)
	}

	err = os.WriteFile(filepath.Join(rootDir, "old", presentationFile), []byte("title: An Older Talk\ntheme: default\n"), 0o644)
	if err != nil {
		t.Fatalf("cannot write the deck: %v", err)
	}

	rec := ask(t, editor, http.MethodGet, "/api/deck?path=old", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a deck of files per slide answered %d: %s", rec.Code, rec.Body.String())
	}

	if !strings.Contains(rec.Body.String(), presentationFile) {
		t.Fatalf("the refusal does not name the file: %s", rec.Body.String())
	}
}

// The editor fetches nothing from the network: its own type comes out of the
// binary, the way the deck's does.
func TestEditorServesItsFonts(t *testing.T) {
	editor, _ := editorOn(t)

	req := httptest.NewRequest(http.MethodGet, "/fonts/ibm-plex-sans-latin.woff2", nil)
	req.Host = "127.0.0.1:8080"

	rec := httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the font answered %d", rec.Code)
	}

	if rec.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("the font is served as %q", rec.Header().Get("Content-Type"))
	}

	req = httptest.NewRequest(http.MethodGet, "/fonts/../theme.css", nil)
	req.Host = "127.0.0.1:8080"

	rec = httptest.NewRecorder()
	editor.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("a path out of the font directory was served")
	}
}
