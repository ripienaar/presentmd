package present

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// reloadBuffer is how many reloads one browser may fall behind by. A reload says
// only that the deck moved, so the one waiting carries the browser to the page
// as it now stands whenever it gets there.
const reloadBuffer = 1

// keepalive is how often a stream with nothing to say writes a comment. A deck
// sits untouched for the length of a talk, and a proxy or a laptop that slept
// closes a connection nothing has written to.
const keepalive = 25 * time.Second

// liveDeck is the deck's reload stream: a revision the handler raises on every
// swap and every browser listening for it.
//
// The board has one of these and it is unexported there, which is deliberate:
// the board mounts this handler, so the import runs from server to present and a
// deck cannot reach back for it.
type liveDeck struct {
	log *slog.Logger

	mu       sync.Mutex
	clients  map[chan uint64]struct{}
	closed   bool
	revision uint64
}

func newLiveDeck(log *slog.Logger) *liveDeck {
	return &liveDeck{log: log, clients: map[chan uint64]struct{}{}}
}

// changed pushes a reload to every listening browser.
func (l *liveDeck) changed() {
	l.mu.Lock()

	if l.closed {
		l.mu.Unlock()

		return
	}

	l.revision++
	revision := l.revision
	waiting := len(l.clients)

	// The sends happen while the lock is held. Copying the channels and sending
	// after letting it go left close free to run in between, closing those same
	// channels, and a reload landing during shutdown panicked sending to one.
	// Each send is already non blocking, so holding the lock costs a map walk.
	for stream := range l.clients {
		select {
		case stream <- revision:
		default:
		}
	}

	l.mu.Unlock()

	l.log.Debug("The deck changed", "revision", revision, "clients", waiting)
}

// subscribe adds one browser to the stream. It returns false once the deck is
// shutting down, so a request arriving then is answered rather than parked on a
// stream nothing will write to.
func (l *liveDeck) subscribe() (chan uint64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil, false
	}

	stream := make(chan uint64, reloadBuffer)
	l.clients[stream] = struct{}{}

	return stream, true
}

func (l *liveDeck) unsubscribe(stream chan uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.clients, stream)
}

// close ends every open stream.
func (l *liveDeck) close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return
	}

	l.closed = true

	for stream := range l.clients {
		close(stream)
		delete(l.clients, stream)
	}
}

// handleEvents is the reload stream. The page's script reloads on any message,
// so the event carries the word and nothing else; EventSource reconnects on its
// own, which is why present serve holds its port across a restart.
func (h *DeckHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	if h.live == nil {
		http.NotFound(w, r)

		return
	}

	stream, ok := h.live.subscribe()
	if !ok {
		http.Error(w, "the deck is shutting down", http.StatusServiceUnavailable)

		return
	}
	defer h.live.unsubscribe(stream)

	control := http.NewResponseController(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	// The comment goes out before anything waits on a change, so the browser's
	// EventSource opens on connect rather than on the first edit.
	err := writeComment(w, control, "open")
	if err != nil {
		return
	}

	ticker := time.NewTicker(keepalive)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			// The tab closed or reloaded. Returning ends the goroutine, so a
			// reload does not leave one behind per refresh.
			return

		case _, ok := <-stream:
			if !ok {
				return
			}

			err := writeReload(w, control)
			if err != nil {
				h.log.Debug("Cannot write to a deck event stream", "error", err)

				return
			}

		case <-ticker.C:
			err := writeComment(w, control, "keepalive")
			if err != nil {
				return
			}
		}
	}
}

func writeReload(w io.Writer, control *http.ResponseController) error {
	_, err := fmt.Fprint(w, "data: reload\n\n")
	if err != nil {
		return err
	}

	return control.Flush()
}

func writeComment(w io.Writer, control *http.ResponseController, note string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", note)
	if err != nil {
		return err
	}

	return control.Flush()
}
