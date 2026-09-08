package present

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// handlerLogger keeps a test's output to the test's own failures.
func handlerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// deckFile is one file written into a temp deck.
type deckFile struct {
	name string
	body string
}

// writeDeck builds a deck directory holding two slides, an image one of them
// names and a file beside them that no slide names. The theme is the embedded
// plain one, so the deck is a directory of files and nothing else.
func writeDeck(t *testing.T, extra ...deckFile) string {
	t.Helper()

	dir := t.TempDir()

	files := []deckFile{
		{name: "presentation.yaml", body: "title: A Deck\ntheme: default\n"},
		{name: "01-title.md", body: "---\npage_style: title\n---\n\n# A Deck\n"},
		{name: "02-image.md", body: "---\npage_style: content\n---\n\n# The Image\n\n<img src=\"images/one.png\">\n"},
		{name: "images/one.png", body: "not really a png"},
		{name: "unlisted.png", body: "a file no slide names"},
	}

	files = append(files, extra...)

	for _, file := range files {
		path := filepath.Join(dir, filepath.FromSlash(file.name))

		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatalf("cannot create %s: %v", filepath.Dir(path), err)
		}

		err = os.WriteFile(path, []byte(file.body), 0o644)
		if err != nil {
			t.Fatalf("cannot write %s: %v", path, err)
		}
	}

	return dir
}

// loadDeckHandler loads the deck in dir and returns a live handler over it.
func loadDeckHandler(t *testing.T, dir string) *DeckHandler {
	t.Helper()

	deck, err := LoadDeck(dir)
	if err != nil {
		t.Fatalf("the deck did not load: %v", err)
	}

	theme, err := LoadTheme(deck.Presentation.Theme, dir)
	if err != nil {
		deck.Close()

		t.Fatalf("the theme did not load: %v", err)
	}

	handler, err := Handler(deck, theme, HandlerOptions{Log: handlerLogger(), Live: true})
	if err != nil {
		deck.Close()

		t.Fatalf("the handler was not built: %v", err)
	}

	t.Cleanup(func() { handler.Close() })

	return handler
}

// get asks the handler for one path.
func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))

	return response
}

func TestHandlerServesTheDeckPage(t *testing.T) {
	handler := loadDeckHandler(t, writeDeck(t))

	response := get(t, handler, "/")
	if response.Code != http.StatusOK {
		t.Fatalf("the page answered %d, want 200", response.Code)
	}

	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("the page is %q, want text/html; charset=utf-8", response.Header().Get("Content-Type"))
	}

	body := response.Body.String()

	for _, want := range []string{
		"<title>A Deck</title>",
		`src="reveal/reveal.js"`,
		`href="theme/theme.css"`,
		`new EventSource("events")`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not hold %s", want)
		}
	}
}

func TestHandlerServesTheVendoredReveal(t *testing.T) {
	handler := loadDeckHandler(t, writeDeck(t))

	response := get(t, handler, "/reveal/reveal.js")
	if response.Code != http.StatusOK {
		t.Fatalf("reveal.js answered %d, want 200", response.Code)
	}

	if response.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Errorf("reveal.js is %q, want text/javascript; charset=utf-8", response.Header().Get("Content-Type"))
	}

	if response.Body.Len() == 0 {
		t.Error("reveal.js was answered empty")
	}

	if get(t, handler, "/reveal/plugin/notes.js").Code != http.StatusOK {
		t.Error("the notes plugin is not served")
	}
}

func TestHandlerServesTheThemeStylesheet(t *testing.T) {
	handler := loadDeckHandler(t, writeDeck(t))

	response := get(t, handler, "/theme/theme.css")
	if response.Code != http.StatusOK {
		t.Fatalf("theme.css answered %d, want 200", response.Code)
	}

	if response.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Errorf("theme.css is %q, want text/css; charset=utf-8", response.Header().Get("Content-Type"))
	}

	if get(t, handler, "/theme/").Code != http.StatusNotFound {
		t.Error("the theme directory is listed rather than refused")
	}
}

// TestHandlerServesADeckAsset pins the one route the deck's own files answer on.
// The renderer writes every slide reference under the asset prefix, so a deck
// holding a directory called reveal or theme keeps its files and nothing beside
// the page answers for them.
func TestHandlerServesADeckAsset(t *testing.T) {
	handler := loadDeckHandler(t, writeDeck(t))

	if get(t, handler, "/images/one.png").Code != http.StatusNotFound {
		t.Error("a deck file answered beside the page, where the vendored reveal and the theme sit")
	}

	for _, path := range []string{"/assets/images/one.png"} {
		response := get(t, handler, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s answered %d, want 200", path, response.Code)
		}

		if response.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s is %q, want image/png", path, response.Header().Get("Content-Type"))
		}

		if response.Body.String() != "not really a png" {
			t.Errorf("%s answered %q", path, response.Body.String())
		}
	}
}

// TestHandlerRefusesAFileTheRenderDidNotName is the rule that keeps a draft or a
// private note in the deck directory from being fetched by anyone who guesses
// its name: the render's asset list is the whole of what the deck answers for.
func TestHandlerRefusesAFileTheRenderDidNotName(t *testing.T) {
	dir := writeDeck(t)
	handler := loadDeckHandler(t, dir)

	_, err := os.Stat(filepath.Join(dir, "unlisted.png"))
	if err != nil {
		t.Fatalf("the unlisted file is not in the deck: %v", err)
	}

	for _, path := range []string{"/assets/unlisted.png", "/unlisted.png", "/assets/presentation.yaml"} {
		response := get(t, handler, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, response.Code)
		}
	}
}

// TestHandlerRefusesAPathThatLeavesTheDeck asks for a file beside the deck by a
// path the mux does not clean, since it arrives escaped. The renderer drops a
// climbing reference before it reaches the asset list, so this is the refusal
// under that one.
func TestHandlerRefusesAPathThatLeavesTheDeck(t *testing.T) {
	dir := writeDeck(t, deckFile{
		name: "03-away.md",
		body: "---\npage_style: content\n---\n\n# Away\n\n<img src=\"../secret.txt\">\n",
	})

	err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("not the deck's"), 0o644)
	if err != nil {
		t.Fatalf("cannot write the file outside the deck: %v", err)
	}

	handler := loadDeckHandler(t, dir)

	for _, asset := range handler.assets {
		if strings.Contains(asset, "..") {
			t.Errorf("the render listed %q, which leaves the deck", asset)
		}
	}

	for _, path := range []string{"/assets/%2e%2e/secret.txt", "/%2e%2e/secret.txt"} {
		response := get(t, handler, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", path, response.Code)
		}

		if strings.Contains(response.Body.String(), "not the deck's") {
			t.Errorf("%s answered with the file outside the deck", path)
		}
	}
}

func TestEventsAnswersAnEventStream(t *testing.T) {
	handler := loadDeckHandler(t, writeDeck(t))

	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/events")
	if err != nil {
		t.Fatalf("the events request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("events answered %d, want 200", response.StatusCode)
	}

	if response.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("events is %q, want text/event-stream", response.Header.Get("Content-Type"))
	}

	if response.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("events caches as %q, want no-store", response.Header.Get("Cache-Control"))
	}

	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("the stream said nothing on connect: %v", err)
	}

	if !strings.HasPrefix(line, ":") {
		t.Errorf("the stream opened with %q, want a comment", line)
	}
}

// TestReloadReachesAnOpenStream pins the whole of live reload: a file changes,
// the watcher settles, the page is rendered again and the browser is told to
// load it.
func TestReloadReachesAnOpenStream(t *testing.T) {
	dir := writeDeck(t)
	handler := loadDeckHandler(t, dir)

	// A real save settles over 300ms, which is three seconds of a test that
	// changes a file five times.
	handler.settle = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go handler.Watch(ctx, dir)

	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL + "/events")
	if err != nil {
		t.Fatalf("the events request failed: %v", err)
	}
	defer response.Body.Close()

	reader := bufio.NewReader(response.Body)

	_, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("the stream said nothing on connect: %v", err)
	}

	err = os.WriteFile(filepath.Join(dir, "02-second.md"), []byte("---\npage_style: content\n---\n\n# Second\n"), 0o644)
	if err != nil {
		t.Fatalf("cannot write the new slide: %v", err)
	}

	reload := make(chan string, 1)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			if strings.HasPrefix(line, "data:") {
				reload <- strings.TrimSpace(line)

				return
			}
		}
	}()

	select {
	case line := <-reload:
		if line != "data: reload" {
			t.Errorf("the stream said %q, want data: reload", line)
		}

	case <-time.After(5 * time.Second):
		t.Fatal("no reload reached the stream after the deck changed")
	}

	page := get(t, handler, "/").Body.String()
	if !strings.Contains(page, "Second") {
		t.Error("the page served after the reload does not hold the new slide")
	}
}

// TestAFailedReloadKeepsTheLastGoodPage is the rule that matters mid-talk: a
// save that leaves the deck unloadable does not blank the slide on the screen.
func TestAFailedReloadKeepsTheLastGoodPage(t *testing.T) {
	dir := writeDeck(t)
	handler := loadDeckHandler(t, dir)

	before := get(t, handler, "/").Body.String()

	err := os.Remove(filepath.Join(dir, "presentation.yaml"))
	if err != nil {
		t.Fatalf("cannot remove presentation.yaml: %v", err)
	}

	handler.reload(dir)

	after := get(t, handler, "/").Body.String()
	if after != before {
		t.Error("a reload that could not load the deck changed the page being served")
	}

	if get(t, handler, "/assets/images/one.png").Code != http.StatusOK {
		t.Error("a reload that failed took the deck's assets with it")
	}
}

// TestAFailedThemeReloadKeepsTheLastGoodPage is the other half: the deck loads
// and the theme it names has gone.
func TestAFailedThemeReloadKeepsTheLastGoodPage(t *testing.T) {
	dir := writeDeck(t)
	handler := loadDeckHandler(t, dir)

	before := get(t, handler, "/").Body.String()

	err := os.WriteFile(filepath.Join(dir, "presentation.yaml"), []byte("title: A Deck\ntheme: nosuchtheme\n"), 0o644)
	if err != nil {
		t.Fatalf("cannot rewrite presentation.yaml: %v", err)
	}

	handler.reload(dir)

	after := get(t, handler, "/").Body.String()
	if after != before {
		t.Error("a reload that could not load the theme changed the page being served")
	}
}

// TestProblemsAreReported pins what present serve writes to stdout: the loader's
// problems and the render's, on the first load as well as on a reload, since the
// design puts no banner on the page.
func TestProblemsAreReported(t *testing.T) {
	dir := writeDeck(t, deckFile{
		name: "03-broken.md",
		body: "---\npage_style: nosuchstyle\n---\n\n# Broken\n",
	})

	deck, err := LoadDeck(dir)
	if err != nil {
		t.Fatalf("the deck did not load: %v", err)
	}

	theme, err := LoadTheme(deck.Presentation.Theme, dir)
	if err != nil {
		t.Fatalf("the theme did not load: %v", err)
	}

	var reported [][]Problem

	handler, err := Handler(deck, theme, HandlerOptions{
		Log:    handlerLogger(),
		Live:   true,
		Report: func(problems []Problem) { reported = append(reported, problems) },
	})
	if err != nil {
		t.Fatalf("the handler was not built: %v", err)
	}
	defer handler.Close()

	if len(reported) != 1 {
		t.Fatalf("the first render reported %d times, want 1", len(reported))
	}

	if len(reported[0]) != 1 || !strings.Contains(reported[0][0].Message, "nosuchstyle") {
		t.Fatalf("the reported problems are %v", reported[0])
	}

	if reported[0][0].Path != "03-broken.md" {
		t.Errorf("the problem names %q, want 03-broken.md", reported[0][0].Path)
	}

	handler.reload(dir)

	if len(reported) != 2 {
		t.Errorf("the reload reported %d times in all, want 2", len(reported))
	}

	if len(handler.Problems()) != 1 {
		t.Errorf("the handler holds %d problems, want 1", len(handler.Problems()))
	}
}

// TestAMountWithNoStreamRendersWithoutTheReloadScript covers a handler running
// neither a stream of its own nor a caller's: the page carries no script and the
// events endpoint is not there. The board sets EventsURL instead, and its deck
// pages reload on the board's own stream.
func TestAMountWithNoStreamRendersWithoutTheReloadScript(t *testing.T) {
	dir := writeDeck(t)

	deck, err := LoadDeck(dir)
	if err != nil {
		t.Fatalf("the deck did not load: %v", err)
	}

	theme, err := LoadTheme(deck.Presentation.Theme, dir)
	if err != nil {
		t.Fatalf("the theme did not load: %v", err)
	}

	handler, err := Handler(deck, theme, HandlerOptions{Log: handlerLogger()})
	if err != nil {
		t.Fatalf("the handler was not built: %v", err)
	}
	defer handler.Close()

	if strings.Contains(get(t, handler, "/").Body.String(), "EventSource") {
		t.Error("a deck nobody is watching carries the reload script")
	}

	if get(t, handler, "/events").Code != http.StatusNotFound {
		t.Error("a deck nobody is watching answers on the events endpoint")
	}
}

func TestContentTypeByExtension(t *testing.T) {
	cases := map[string]string{
		"theme.css":        "text/css; charset=utf-8",
		"reveal.js":        "text/javascript; charset=utf-8",
		"images/one.png":   "image/png",
		"images/two.SVG":   "image/svg+xml",
		"fonts/one.woff2":  "font/woff2",
		"video/three.mp4":  "video/mp4",
		"something.nosuch": "application/octet-stream",
	}

	for name, want := range cases {
		if contentType(name) != want {
			t.Errorf("%s is %q, want %q", name, contentType(name), want)
		}
	}
}

// TestHandlerServesADeckDirectoryNamedReveal is why deck files sit under a path
// segment of their own. A deck may hold a directory called reveal or theme, and
// without the prefix those requests were answered by the vendored reveal or by
// the theme rather than by the deck.
func TestHandlerServesADeckDirectoryNamedReveal(t *testing.T) {
	dir := writeDeck(t,
		deckFile{name: "03-shadow.md", body: "---\npage_style: content\n---\n\n# Shadowed\n\n<img src=\"reveal/logo.png\">\n<img src=\"theme/logo.png\">\n"},
		deckFile{name: "reveal/logo.png", body: "the deck's own reveal directory"},
		deckFile{name: "theme/logo.png", body: "the deck's own theme directory"},
	)

	handler := loadDeckHandler(t, dir)

	for _, tc := range []struct{ path, want string }{
		{path: "/assets/reveal/logo.png", want: "the deck's own reveal directory"},
		{path: "/assets/theme/logo.png", want: "the deck's own theme directory"},
	} {
		response := get(t, handler, tc.path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s answered %d, want 200", tc.path, response.Code)
		}

		if response.Body.String() != tc.want {
			t.Errorf("%s answered %q, want the deck's file", tc.path, response.Body.String())
		}
	}

	// The vendored reveal still answers on its own route, which is the collision
	// the prefix exists to keep apart.
	if get(t, handler, "/reveal/reveal.js").Code != http.StatusOK {
		t.Error("the vendored reveal stopped answering")
	}
}

// TestReloadWatchesADirectoryThemeAdoptedLater pins that switching a deck onto a
// directory theme starts watching that directory. The theme is named by writing
// presentation.yaml, which is a write rather than a create, so the watch set has
// to be read again after a reload and not only when a file appears.
func TestReloadWatchesADirectoryThemeAdoptedLater(t *testing.T) {
	dir := writeDeck(t)

	themeDir := filepath.Join(dir, "mytheme")

	err := os.MkdirAll(filepath.Join(themeDir, "styles"), 0o755)
	if err != nil {
		t.Fatalf("cannot create the theme directory: %v", err)
	}

	for name, body := range map[string]string{
		"theme.yaml":         "code_style: bw\n",
		"theme.css":          ".reveal { color: black; }\n",
		"styles/title.jet":   "<h1>{{ raw: heading }}</h1>\n",
		"styles/content.jet": "<div class=\"body\">{{ raw: body }}</div>\n",
	} {
		err = os.WriteFile(filepath.Join(themeDir, filepath.FromSlash(name)), []byte(body), 0o644)
		if err != nil {
			t.Fatalf("cannot write %s: %v", name, err)
		}
	}

	handler := loadDeckHandler(t, dir)

	err = os.WriteFile(filepath.Join(dir, "presentation.yaml"), []byte("title: A Deck\ntheme: ./mytheme\n"), 0o644)
	if err != nil {
		t.Fatalf("cannot name the directory theme: %v", err)
	}

	handler.reload(dir)

	handler.mu.RLock()
	theme := handler.theme
	handler.mu.RUnlock()

	if theme == nil || theme.Dir == "" {
		t.Fatal("the deck did not switch to the directory theme")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("cannot build a watcher: %v", err)
	}
	defer watcher.Close()

	handler.watchDirs(watcher, dir)

	watched := watcher.WatchList()

	found := false

	for _, path := range watched {
		if path == themeDir {
			found = true
		}
	}

	if !found {
		t.Errorf("the theme directory %s is not watched, watching %v", themeDir, watched)
	}
}
