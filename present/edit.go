package present

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
)

const (
	// tokenHeader carries the token the editor page was served with. Every api
	// request has to send it, which is what a page on another origin cannot do:
	// it may post to this server, but the browser will not let it read the page
	// that holds the token.
	tokenHeader = "X-Presentmd-Token"
	// previewPrefix is where the deck being edited is served. The editor page
	// holds it in an iframe, so the deck under the editor is the deck reveal
	// renders rather than a drawing of one.
	previewPrefix = "/preview"
	// draftLimit is the largest deck body the editor accepts. A talk is prose and
	// yaml; anything past this is not one.
	draftLimit = 8 << 20
	// deckDepth is how far under the root a deck is looked for. A tree of talks
	// is a few directories deep, and walking a whole home directory because the
	// root was typed wide is not worth doing.
	deckDepth = 4
	// deckLimit is how many decks a listing carries.
	deckLimit = 500
	// draftName is the file a save writes before it renames it over the deck. A
	// save caught halfway leaves this behind rather than a half written talk.
	draftName = ".presentation.md.saving"
	// newDeckTheme is the theme a deck started in the editor names. A deck has to
	// name one and this is the one that ships in the binary.
	newDeckTheme = "default"
	// sessionMark is where the page carries what the editor served it with: the
	// token every api request repeats, the root and the deck it opened on.
	sessionMark = "{{session}}"
)

// editorPage is the editor itself. It is one file: the page, its stylesheet and
// its script, so the editor loads with nothing fetched.
//
//go:embed editor.html
var editorPage []byte

// ErrEditRoot is returned when the editor is built without a root. Every path it
// reads and writes is resolved through one, so there is no editor without it.
var ErrEditRoot = errors.New("the editor needs a root directory")

// deckNameRe is the name a new deck directory may take: one path segment of the
// characters a directory beside a talk carries, and nothing that reads as a
// path.
var deckNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// EditorOptions are the settings the editor takes.
type EditorOptions struct {
	// Log is where the editor writes. It is slog.Default() when nil.
	Log *slog.Logger
	// Root is the directory every read and every write is confined to. The editor
	// opens nothing outside it: a deck is named as a path relative to it, and
	// os.Root refuses a name that climbs out of it and a symlink that points out
	// of it. The editor does not close it; the caller that opened it does.
	Root *os.Root
	// Deck is the deck open when the editor starts, relative to Root, "." for the
	// root directory itself.
	Deck string
	// Theme is the theme the preview renders with instead of the one the deck
	// names, empty when the deck decides.
	Theme string
}

// Editor serves the deck editor: the page at the root of its space, the deck
// being edited under a preview prefix, and a small api between them.
//
// It holds one deck open at a time. Every keystroke that reaches it is encoded
// as a presentation.md, read back through the loader and rendered into the
// preview, so the deck under the editor is the file a save would write.
type Editor struct {
	log   *slog.Logger
	mux   *http.ServeMux
	root  *os.Root
	deck  *DeckHandler
	page  []byte
	token string
	// themeName is the caller's theme override, empty when the deck names its
	// own.
	themeName string

	mu sync.Mutex
	// open is the deck being edited, relative to the root.
	open string
}

// NewEditor builds the editor on root and opens the deck opts.Deck names. A deck
// directory that holds no presentation.md yet opens empty, which is what a new
// talk starts as.
func NewEditor(opts EditorOptions) (*Editor, error) {
	if opts.Root == nil {
		return nil, ErrEditRoot
	}

	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	open := opts.Deck
	if open == "" {
		open = "."
	}

	if !fs.ValidPath(open) {
		return nil, fmt.Errorf("%q is not a deck below the root", open)
	}

	token := make([]byte, 16)

	_, err := rand.Read(token)
	if err != nil {
		return nil, err
	}

	e := &Editor{
		log:       log,
		mux:       http.NewServeMux(),
		root:      opts.Root,
		token:     hex.EncodeToString(token),
		themeName: opts.Theme,
		open:      open,
	}

	e.page, err = e.buildPage()
	if err != nil {
		return nil, err
	}

	deck, theme, err := e.load(open)
	if err != nil {
		return nil, err
	}

	e.deck, err = Handler(deck, theme, HandlerOptions{Log: log, Live: true})
	if err != nil {
		deck.Close()

		return nil, err
	}

	e.mux.HandleFunc("GET /{$}", e.handlePage)
	e.mux.Handle(previewPrefix+"/", http.StripPrefix(previewPrefix, e.deck))
	e.mux.HandleFunc("GET /fonts/{name}", e.handleFont)
	e.mux.HandleFunc("GET /api/deck", e.guarded(e.handleDeck))
	e.mux.HandleFunc("GET /api/decks", e.guarded(e.handleDecks))
	e.mux.HandleFunc("POST /api/decks", e.guarded(e.handleCreate))
	e.mux.HandleFunc("POST /api/draft", e.guarded(e.handleDraft))
	e.mux.HandleFunc("POST /api/save", e.guarded(e.handleSave))

	return e, nil
}

func (e *Editor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Every request, the page included, has to arrive on a loopback name. A page
	// on the network that resolved its own name to this machine would otherwise
	// reach an editor that writes files.
	if !loopbackHost(r.Host) {
		e.log.Warn("Refused a request that did not arrive on a loopback address", "host", r.Host, "path", r.URL.Path)
		http.Error(w, "the editor answers on the loopback address only", http.StatusForbidden)

		return
	}

	e.mux.ServeHTTP(w, r)
}

// Close ends the preview's event streams and releases the deck it holds. The
// root belongs to the caller.
func (e *Editor) Close() error {
	return e.deck.Close()
}

// CloseStreams ends every open event stream, so a shutdown does not wait out the
// grace period on the browser holding the editor open.
func (e *Editor) CloseStreams() {
	e.deck.CloseStreams()
}

// Deck is the deck the editor has open, relative to the root.
func (e *Editor) Deck() string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.open
}

// Root is the directory the editor is confined to.
func (e *Editor) Root() string {
	return e.root.Name()
}

// guarded refuses an api request that did not come from the page this editor
// served. The token is the check that matters: another origin may post here, but
// the browser will not hand it the page the token was written into, so it cannot
// send one. The origin header is checked as well where the browser sent one.
func (e *Editor) guarded(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(tokenHeader) != e.token {
			e.log.Warn("Refused an editor request with no token", "path", r.URL.Path, "origin", r.Header.Get("Origin"))
			http.Error(w, "this request did not come from the editor", http.StatusForbidden)

			return
		}

		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+r.Host {
			e.log.Warn("Refused an editor request from another origin", "path", r.URL.Path, "origin", origin)
			http.Error(w, "this request did not come from the editor", http.StatusForbidden)

			return
		}

		next(w, r)
	}
}

// loopbackHost says whether a request's host is this machine reached by a name
// that cannot be pointed anywhere else.
func loopbackHost(host string) bool {
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = host
	}

	name = strings.Trim(name, "[]")

	if name == "localhost" {
		return true
	}

	ip := net.ParseIP(name)

	return ip != nil && ip.IsLoopback()
}

// deckPayload is one deck as the editor page holds it: what a save writes back,
// what the loader made of it, and the file's own revision.
type deckPayload struct {
	// Path is the deck relative to the root, "." for the root itself.
	Path string `json:"path"`
	// Root is the directory the editor is confined to, for the page to show.
	Root         string       `json:"root"`
	Presentation Presentation `json:"presentation"`
	Slides       []*Slide     `json:"slides"`
	Problems     []Problem    `json:"problems"`
	// Revision is the modification time of presentation.md in nanoseconds, and
	// zero for a deck that has not been written yet. A save carries the revision
	// it read, so a file that changed underneath the browser is reported rather
	// than written over. It crosses as a string: a modification time in
	// nanoseconds is past the integer a browser holds exactly, and one carried as
	// a number came back rounded, so every save read as a file that had changed.
	Revision int64 `json:"revision,string"`
	// Markdown is the file as it stands, which is what the copy button hands over.
	Markdown string `json:"markdown"`
}

// deckRequest is a draft or a save: the deck as the browser holds it.
type deckRequest struct {
	Path         string       `json:"path"`
	Presentation Presentation `json:"presentation"`
	Slides       []*Slide     `json:"slides"`
	// Revision is the revision the browser opened, checked on a save, carried as
	// a string for the reason deckPayload gives.
	Revision int64 `json:"revision,string"`
	// Force writes over a file that changed on disk since it was opened.
	Force bool `json:"force"`
	// Name is the directory a new deck is created in, under the root.
	Name string `json:"name,omitempty"`
}

// deckEntry is one deck of the listing the open menu shows.
type deckEntry struct {
	Path   string `json:"path"`
	Title  string `json:"title"`
	Slides int    `json:"slides"`
}

// buildPage writes the session into the page: the token every api request
// carries, the root the editor is confined to and the deck it opened on. The
// token is written into the page rather than answered from an endpoint, so a
// page on another origin has nowhere to read it from.
func (e *Editor) buildPage() ([]byte, error) {
	session, err := json.Marshal(map[string]string{"token": e.token, "root": e.root.Name(), "deck": e.open})
	if err != nil {
		return nil, err
	}

	page := bytes.Replace(editorPage, []byte(sessionMark), session, 1)
	if bytes.Equal(page, editorPage) {
		return nil, errors.New("the editor page carries no session placeholder")
	}

	return page, nil
}

func (e *Editor) handlePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	_, err := w.Write(e.page)
	if err != nil {
		e.log.Debug("Cannot write the editor page", "error", err)
	}
}

// handleFont answers the editor's own type out of the fonts the shipped theme
// carries. The page fetches nothing from the network, so a talk is written the
// same on a machine that has never installed IBM Plex and on one with no network
// at all.
func (e *Editor) handleFont(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	data, err := themeFiles.ReadFile(path.Join(embeddedDir, newDeckTheme, "fonts", name))
	if err != nil {
		http.NotFound(w, r)

		return
	}

	w.Header().Set("Content-Type", contentType(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "max-age=86400")

	_, err = w.Write(data)
	if err != nil {
		e.log.Debug("Cannot write a font", "name", name, "error", err)
	}
}

func (e *Editor) handleDeck(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		rel = e.Deck()
	}

	payload, err := e.payload(rel)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	// Opening a deck is what moves the preview onto it, so the deck under the
	// editor is the deck in it.
	err = e.show(rel, payload.Presentation, payload.Slides)
	if err != nil {
		e.log.Warn("Cannot render the deck that was opened", "deck", rel, "error", err)
	}

	e.mu.Lock()
	e.open = rel
	e.mu.Unlock()

	e.write(w, payload)
}

func (e *Editor) handleDecks(w http.ResponseWriter, r *http.Request) {
	e.write(w, map[string]any{"root": e.root.Name(), "decks": e.decks()})
}

// handleCreate makes a deck directory under the root and writes the presentation
// the browser filled in. It creates one directory, directly under the root, with
// a name that is one path segment: a talk started here cannot land anywhere the
// root does not already reach.
func (e *Editor) handleCreate(w http.ResponseWriter, r *http.Request) {
	req, err := readRequest(r)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	if !deckNameRe.MatchString(req.Name) {
		e.fail(w, http.StatusBadRequest, fmt.Errorf("%q is not a directory name for a deck: letters, digits, dot, dash and underscore", req.Name))

		return
	}

	err = e.root.Mkdir(req.Name, 0o755)
	if err != nil {
		e.fail(w, http.StatusConflict, fmt.Errorf("cannot make the deck directory: %w", err))

		return
	}

	data, err := EncodeDeck(req.Presentation, req.Slides)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	err = e.root.WriteFile(path.Join(req.Name, presentationMarkdownFile), data, 0o644)
	if err != nil {
		e.fail(w, http.StatusInternalServerError, fmt.Errorf("cannot write the deck: %w", err))

		return
	}

	e.log.Info("Made a deck", "deck", req.Name, "root", e.root.Name())

	payload, err := e.payload(req.Name)
	if err != nil {
		e.fail(w, http.StatusInternalServerError, err)

		return
	}

	err = e.show(req.Name, payload.Presentation, payload.Slides)
	if err != nil {
		e.log.Warn("Cannot render the deck that was made", "deck", req.Name, "error", err)
	}

	e.mu.Lock()
	e.open = req.Name
	e.mu.Unlock()

	e.write(w, payload)
}

// handleDraft renders what the browser holds without writing anything. It is
// every keystroke that settled, so the deck in the frame below the editor is the
// deck being typed.
func (e *Editor) handleDraft(w http.ResponseWriter, r *http.Request) {
	req, err := readRequest(r)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	rel, err := e.deckPath(req.Path)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	markdown, problems := e.draft(rel, req.Presentation, req.Slides)

	e.write(w, map[string]any{"markdown": string(markdown), "problems": problems})
}

// handleSave writes the deck to presentation.md. The bytes written are the bytes
// the preview was rendered from, and they are written to a file beside the deck
// and renamed over it, so a save caught halfway leaves the talk that was there.
func (e *Editor) handleSave(w http.ResponseWriter, r *http.Request) {
	req, err := readRequest(r)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	rel, err := e.deckPath(req.Path)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	data, err := EncodeDeck(req.Presentation, req.Slides)
	if err != nil {
		e.fail(w, http.StatusBadRequest, err)

		return
	}

	name := path.Join(rel, presentationMarkdownFile)

	revision, err := e.revision(name)
	if err != nil {
		e.fail(w, http.StatusInternalServerError, err)

		return
	}

	if !req.Force && revision != req.Revision {
		e.fail(w, http.StatusConflict, errors.New("presentation.md changed on disk since it was opened"))

		return
	}

	temp := path.Join(rel, draftName)

	err = e.root.WriteFile(temp, data, 0o644)
	if err != nil {
		e.fail(w, http.StatusInternalServerError, fmt.Errorf("cannot write the deck: %w", err))

		return
	}

	err = e.root.Rename(temp, name)
	if err != nil {
		e.fail(w, http.StatusInternalServerError, fmt.Errorf("cannot write the deck: %w", err))

		return
	}

	revision, err = e.revision(name)
	if err != nil {
		e.fail(w, http.StatusInternalServerError, err)

		return
	}

	_, problems := e.draft(rel, req.Presentation, req.Slides)

	e.log.Info("Wrote a deck", "deck", name, "root", e.root.Name(), "slides", len(req.Slides), "problems", len(problems))

	e.write(w, map[string]any{"revision": strconv.FormatInt(revision, 10), "markdown": string(data), "problems": problems})
}

// draft renders a deck into the preview and returns what it would be written as.
// A deck that will not encode, load a theme or render keeps the preview that is
// already up, the way a broken save keeps the page a talk is standing on, and
// says what was wrong.
func (e *Editor) draft(rel string, presentation Presentation, slides []*Slide) ([]byte, []Problem) {
	data, err := EncodeDeck(presentation, slides)
	if err != nil {
		return nil, []Problem{{Path: presentationMarkdownFile, Message: err.Error()}}
	}

	deck, theme, err := e.decode(rel, data)
	if err != nil {
		return data, []Problem{{Path: presentationMarkdownFile, Message: err.Error()}}
	}

	err = e.deck.Update(deck, theme)
	if err != nil {
		deck.Close()

		return data, []Problem{{Path: presentationMarkdownFile, Message: fmt.Sprintf("cannot render the deck: %s", err)}}
	}

	e.deck.Reload()

	return data, e.deck.Problems()
}

// show puts a deck into the preview and reports nothing: the payload the caller
// is about to write already carries the problems the loader found.
func (e *Editor) show(rel string, presentation Presentation, slides []*Slide) error {
	data, err := EncodeDeck(presentation, slides)
	if err != nil {
		return err
	}

	deck, theme, err := e.decode(rel, data)
	if err != nil {
		return err
	}

	err = e.deck.Update(deck, theme)
	if err != nil {
		deck.Close()

		return err
	}

	e.deck.Reload()

	return nil
}

// decode reads a presentation.md back into a deck and loads the theme it names.
// The deck holds a root of its own on the deck directory, opened through the
// editor's, so an image a slide names is served from the deck and a path that
// climbs out of it is refused.
func (e *Editor) decode(rel string, data []byte) (*Deck, *Theme, error) {
	sub, err := e.root.OpenRoot(rel)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open the deck directory: %w", err)
	}

	dir := e.dir(rel)
	deck := DecodeDeck(dir, sub, data)

	name := deck.Presentation.Theme
	if e.themeName != "" {
		name = e.themeName
	}

	theme, err := LoadTheme(name, dir)
	if err != nil {
		deck.Close()

		return nil, nil, fmt.Errorf("cannot load the theme: %w", err)
	}

	return deck, theme, nil
}

// load reads the deck at rel off disk, for the editor to open on. A directory
// with no presentation.md is a deck that has not been written yet and loads
// empty, which is what New deck leaves behind until the first save.
func (e *Editor) load(rel string) (*Deck, *Theme, error) {
	payload, err := e.payload(rel)
	if err != nil {
		return nil, nil, err
	}

	data, err := EncodeDeck(payload.Presentation, payload.Slides)
	if err != nil {
		return nil, nil, err
	}

	return e.decode(rel, data)
}

// payload reads one deck through the root. Everything about it comes from the
// loader, so a deck opened in the editor carries the same problems it carries
// when it is served.
func (e *Editor) payload(rel string) (*deckPayload, error) {
	rel, err := e.deckPath(rel)
	if err != nil {
		return nil, err
	}

	sub, err := e.root.OpenRoot(rel)
	if err != nil {
		return nil, fmt.Errorf("cannot open the deck directory: %w", err)
	}

	data, err := sub.ReadFile(presentationMarkdownFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		sub.Close()

		return nil, fmt.Errorf("cannot read the deck: %w", err)
	}

	// A deck written as presentation.yaml with a file per slide is refused rather
	// than opened empty. The editor writes one file, and a save into a directory
	// already holding the other form would leave two decks in it, neither of them
	// the talk.
	if err != nil {
		_, yamlErr := sub.Stat(presentationFile)
		if yamlErr == nil {
			sub.Close()

			return nil, fmt.Errorf("%s is written as %s with a file per slide, which the editor does not write: serve it, or move it to one %s first", rel, presentationFile, presentationMarkdownFile)
		}
	}

	deck := DecodeDeck(e.dir(rel), sub, data)
	defer deck.Close()

	revision, err := e.revision(path.Join(rel, presentationMarkdownFile))
	if err != nil {
		return nil, err
	}

	// A deck that was never written carries the loader's complaint about a
	// presentation with no theme, which is not something to put in front of
	// someone who has not typed anything yet.
	problems := deck.Problems
	if revision == 0 {
		problems = nil
		deck.Presentation.Theme = newDeckTheme
	}

	return &deckPayload{
		Path:         rel,
		Root:         e.root.Name(),
		Presentation: deck.Presentation,
		Slides:       deck.Slides,
		Problems:     problems,
		Revision:     revision,
		Markdown:     string(data),
	}, nil
}

// decks are the decks under the root: every directory holding a presentation.md,
// the root itself included.
func (e *Editor) decks() []deckEntry {
	var found []deckEntry

	err := fs.WalkDir(e.root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if !entry.IsDir() {
			return nil
		}

		// A .git beside a talk is thousands of files and none of them is a deck.
		if name != "." && strings.HasPrefix(entry.Name(), ".") {
			return fs.SkipDir
		}

		if name != "." && strings.Count(name, "/")+1 > deckDepth {
			return fs.SkipDir
		}

		data, err := fs.ReadFile(e.root.FS(), path.Join(name, presentationMarkdownFile))
		if err != nil {
			return nil
		}

		found = append(found, deckEntry{Path: name, Title: deckTitle(data), Slides: strings.Count(string(data), "\n"+slideBreak+"\n") / 2})

		if len(found) >= deckLimit {
			return fs.SkipAll
		}

		return nil
	})
	if err != nil {
		e.log.Warn("Cannot list the decks under the root", "root", e.root.Name(), "error", err)
	}

	return found
}

// deckTitle is what a listed deck is called: the title of its presentation, and
// nothing when the file will not parse, since a listing is not the place a
// broken deck is reported.
func deckTitle(data []byte) string {
	front, _, err := splitFrontmatter(data)
	if err != nil {
		return ""
	}

	var presentation Presentation

	err = yaml.Unmarshal([]byte(front), &presentation)
	if err != nil {
		return ""
	}

	return presentation.Title
}

// revision is the modification time of a file in nanoseconds, and zero for one
// that is not there yet.
func (e *Editor) revision(name string) (int64, error) {
	info, err := e.root.Stat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("cannot read the deck: %w", err)
	}

	return info.ModTime().UnixNano(), nil
}

// deckPath is the deck a request named, checked before it is handed to the root.
// The root refuses a path that leaves it either way; this is what makes the
// refusal say which deck it was about.
func (e *Editor) deckPath(rel string) (string, error) {
	if rel == "" {
		return e.Deck(), nil
	}

	if !fs.ValidPath(rel) {
		return "", fmt.Errorf("%q is not a deck below the root", rel)
	}

	return rel, nil
}

// dir is the absolute path of a deck, which is what a theme's own relative path
// is resolved against and what a problem names.
func (e *Editor) dir(rel string) string {
	return filepath.Join(e.root.Name(), filepath.FromSlash(rel))
}

func readRequest(r *http.Request) (*deckRequest, error) {
	var req deckRequest

	err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, draftLimit)).Decode(&req)
	if err != nil {
		return nil, fmt.Errorf("cannot read the deck that was sent: %w", err)
	}

	return &req, nil
}

func (e *Editor) write(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	err := json.NewEncoder(w).Encode(payload)
	if err != nil {
		e.log.Debug("Cannot write an editor response", "error", err)
	}
}

// fail answers with the message a person reads in the editor. The editor is one
// person's own tool on their own machine, so the reason is theirs to see.
func (e *Editor) fail(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)

	writeErr := json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
	if writeErr != nil {
		e.log.Debug("Cannot write an editor error", "error", writeErr)
	}
}
