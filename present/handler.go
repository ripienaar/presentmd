package present

import (
	"bytes"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// eventsRef is where the page listens for the reload, written relative to the
	// page so one rendering works at / under present serve and under the board's
	// deck path.
	eventsRef = "events"

	// assetPrefix is the route deck assets answer on. It exists so a deck holding
	// a directory named reveal or theme is still reachable, since those two names
	// are taken at the top of the deck's own space.
	assetPrefix = "/assets/"
)

// HandlerOptions are the settings a deck handler takes.
type HandlerOptions struct {
	// Log is where the handler writes. It is slog.Default() when nil.
	Log *slog.Logger
	// Live turns on the events endpoint and the reload script the page carries.
	// The board mounts a deck without it, since a request there renders the deck
	// as it stands and the stream it reloads on is the board's own.
	Live bool
	// EventsURL is where the page listens for the reload when the caller runs the
	// stream itself. The board sets it to the board's own events endpoint: the
	// change event there carries a revision and no path, so a deck page reloads
	// on any change to the planning tree. Live wins over it, since a handler
	// serving its own stream points the page at that.
	EventsURL string
	// Report is called with the problems of every render, the first one included.
	// present serve writes them to stdout there, which is the whole record a
	// person presenting gets: the design puts no banner on the page.
	Report func(problems []Problem)
	// Theme is the theme a reload loads instead of the one presentation.yaml
	// names, empty when the deck decides. A caller overriding the theme passes it
	// here as well as loading the first one itself, since the watcher reads the
	// deck again on every save and would otherwise put the deck's own theme back
	// the first time a slide is edited.
	Theme string
}

// DeckHandler serves one deck: the page at the root of its space, reveal and the
// theme beside it, and the deck's own files under an asset prefix.
//
// The page it answers with is rendered once and swapped by Update, rather than
// rendered per request, so a browser that reloads mid-talk waits on nothing and
// a deck that will not load keeps serving the last page that did.
type DeckHandler struct {
	log  *slog.Logger
	mux  *http.ServeMux
	live *liveDeck
	// settle is the watcher's debounce, held here rather than read off the const
	// so a test does not wait out a real save.
	settle time.Duration
	report func(problems []Problem)
	// eventsURL is the caller's own stream, used when this handler runs none.
	eventsURL string
	// themeName is the caller's theme override, empty when the deck names its
	// own.
	themeName string

	mu       sync.RWMutex
	deck     *Deck
	theme    *Theme
	page     []byte
	assets   []string
	problems []Problem
}

// Handler renders deck through theme and returns the handler that serves it. The
// handler takes the deck: Close releases it, and Update closes the one it
// replaces, so the assets of a page nobody is being served are not held open.
func Handler(deck *Deck, theme *Theme, opts HandlerOptions) (*DeckHandler, error) {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	h := &DeckHandler{
		log:       log,
		mux:       http.NewServeMux(),
		settle:    deckSettle,
		report:    opts.Report,
		eventsURL: opts.EventsURL,
		themeName: opts.Theme,
	}

	if opts.Live {
		h.live = newLiveDeck(log)
	}

	err := h.Update(deck, theme)
	if err != nil {
		return nil, err
	}

	h.mux.HandleFunc("GET /{$}", h.handlePage)
	h.mux.HandleFunc("GET /events", h.handleEvents)
	h.mux.HandleFunc("GET /reveal/{path...}", h.handleReveal)
	h.mux.HandleFunc("GET /theme/{path...}", h.handleTheme)
	h.mux.HandleFunc("GET "+assetPrefix+"{path...}", h.handleAsset)

	return h, nil
}

func (h *DeckHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// Update renders deck through theme and swaps the page every later request is
// answered with. On success the handler takes deck and closes the one it held.
// On a failure the page it is already serving stays, and the caller closes the
// deck it passed: a save that leaves the yaml half written should not blank a
// talk mid-sentence, and the save after it puts the deck back.
func (h *DeckHandler) Update(deck *Deck, theme *Theme) error {
	rendered, err := RenderDeck(deck, theme, h.renderOptions())
	if err != nil {
		return err
	}

	problems := make([]Problem, 0, len(deck.Problems)+len(rendered.Problems))
	problems = append(problems, deck.Problems...)
	problems = append(problems, rendered.Problems...)

	h.mu.Lock()

	held := h.deck

	h.deck = deck
	h.theme = theme
	h.page = []byte(rendered.HTML)
	h.assets = rendered.Assets
	h.problems = problems

	h.mu.Unlock()

	if held != nil && held != deck {
		err := held.Close()
		if err != nil {
			h.log.Debug("Cannot close the deck that was replaced", "dir", held.Dir, "error", err)
		}
	}

	if h.report != nil && len(problems) > 0 {
		h.report(problems)
	}

	return nil
}

// Problems are the problems of the page being served, the loader's and the
// renderer's together.
func (h *DeckHandler) Problems() []Problem {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return slices.Clone(h.problems)
}

// Reload tells every browser on the deck to load the page again. Reveal is
// initialized with hash: true, so the browser comes back to the slide it was
// showing rather than to the front of the talk.
func (h *DeckHandler) Reload() {
	if h.live == nil {
		return
	}

	h.live.changed()
}

// CloseStreams ends every open event stream. An http.Server shutdown waits for
// in flight requests and an event stream is in flight for as long as its tab is
// open, so the streams end first and the handlers return.
func (h *DeckHandler) CloseStreams() {
	if h.live == nil {
		return
	}

	h.live.close()
}

// Close ends the event streams and releases the deck directory.
func (h *DeckHandler) Close() error {
	h.CloseStreams()

	h.mu.Lock()
	deck := h.deck
	h.deck = nil
	h.mu.Unlock()

	if deck == nil {
		return nil
	}

	return deck.Close()
}

// renderOptions are what this handler renders a deck with. Every reference the
// page writes stays relative to the page itself, which is what lets one
// rendering serve at / under present serve and under the board's deck path.
func (h *DeckHandler) renderOptions() RenderOptions {
	if h.live == nil {
		return RenderOptions{EventsURL: h.eventsURL}
	}

	return RenderOptions{EventsURL: eventsRef}
}

func (h *DeckHandler) handlePage(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	page := h.page
	h.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	_, err := w.Write(page)
	if err != nil {
		h.log.Debug("Cannot write the deck page", "error", err)
	}
}

func (h *DeckHandler) handleReveal(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, RevealFS(), r.PathValue("path"), "reveal")
}

func (h *DeckHandler) handleTheme(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	theme := h.theme
	h.mu.RUnlock()

	if theme == nil {
		http.NotFound(w, r)

		return
	}

	h.serveFile(w, r, theme.FS(), r.PathValue("path"), "theme")
}

// handleAsset answers one file of the deck directory. Two things have to hold
// for it to be served: the render named it, and the deck root can read it.
func (h *DeckHandler) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")

	// A reference is listed as the slide wrote it, so ../secret.png in a slide
	// reaches the list and is refused here. The deck root refuses it again on the
	// read below, which is the confinement rather than this check.
	if !fs.ValidPath(name) {
		h.log.Warn("Refused an asset path that leaves the deck", "path", name)
		http.NotFound(w, r)

		return
	}

	// The file is read while the lock is held rather than through a deck pointer
	// carried out from under it. A reload closes the deck it replaced, so a save
	// during an image fetch turned that image into a 404.
	h.mu.RLock()

	listed := slices.Contains(h.assets, name)
	deck := h.deck

	var (
		data []byte
		err  error
	)

	if listed && deck != nil {
		data, err = fs.ReadFile(deck.Root().FS(), name)
	}

	h.mu.RUnlock()

	// A file the render did not name is not part of the deck, whatever sits at
	// that path. The draft nobody linked and the notes beside the slides are in
	// the deck directory and are nobody's to fetch by guessing the name.
	if !listed || deck == nil {
		h.log.Debug("Refused an asset the deck does not name", "path", name)
		http.NotFound(w, r)

		return
	}

	if err != nil {
		h.log.Debug("Cannot read a file the deck was asked for", "kind", "asset", "path", name, "error", err)
		http.NotFound(w, r)

		return
	}

	h.writeFile(w, r, name, data)
}

// serveFile answers one file out of fsys. The bytes are read rather than
// streamed, which is what lets the embedded reveal set, a directory theme and
// the deck root be served through one path; reveal's largest file is under half
// a megabyte and a deck's images are what a browser would hold anyway.
func (h *DeckHandler) serveFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string, kind string) {
	if !fs.ValidPath(name) || name == "." {
		http.NotFound(w, r)

		return
	}

	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		h.log.Debug("Cannot read a file the deck was asked for", "kind", kind, "path", name, "error", err)
		http.NotFound(w, r)

		return
	}

	h.writeFile(w, r, name, data)
}

// writeFile answers with bytes already read, which is what lets the deck's own
// files be read while the lock that guards the deck is held and written after
// it is let go.
func (h *DeckHandler) writeFile(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	w.Header().Set("Content-Type", contentType(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	// The modification time is left zero: a live deck is re-read on every change
	// and a browser holding a stale copy of a slide's image is the failure this
	// avoids. ServeContent still answers a range request, which is what a video
	// background needs.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// contentTypes are the types the deck's own files are served as. The mime
// package builds its table from the machine's, where a missing entry answers a
// font or an svg as application/octet-stream and the browser drops it, so the
// extensions a deck and a theme actually use are written out.
var contentTypes = map[string]string{
	".avif":  "image/avif",
	".css":   "text/css; charset=utf-8",
	".gif":   "image/gif",
	".htm":   "text/html; charset=utf-8",
	".html":  "text/html; charset=utf-8",
	".ico":   "image/x-icon",
	".jpeg":  "image/jpeg",
	".jpg":   "image/jpeg",
	".js":    "text/javascript; charset=utf-8",
	".json":  "application/json",
	".m4v":   "video/mp4",
	".mjs":   "text/javascript; charset=utf-8",
	".mp4":   "video/mp4",
	".otf":   "font/otf",
	".pdf":   "application/pdf",
	".png":   "image/png",
	".svg":   "image/svg+xml",
	".ttf":   "font/ttf",
	".txt":   "text/plain; charset=utf-8",
	".webm":  "video/webm",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))

	known, ok := contentTypes[ext]
	if ok {
		return known
	}

	byExt := mime.TypeByExtension(ext)
	if byExt != "" {
		return byExt
	}

	return "application/octet-stream"
}
