package present

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// deckSettle is how long a change has to settle before the deck is read again.
// An editor saving one slide emits several events and a sync client emits more,
// and one save should produce one reload rather than three.
const deckSettle = 300 * time.Millisecond

// Watch reloads the deck whenever a file under dir changes, and whenever the
// theme's own directory changes for a directory theme, until ctx is canceled. It
// blocks, so the caller runs it beside the server.
//
// A reload that fails leaves the page already being served in place and logs
// what was wrong with the deck, which is what a person mid-talk needs: the
// terminal says the yaml is broken and the slide still shows.
func (h *DeckHandler) Watch(ctx context.Context, dir string) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		h.log.Warn("Cannot start a file watcher, the deck will not reload on a change", "dir", dir, "error", err)

		<-ctx.Done()

		return
	}
	defer fsw.Close()

	h.watchDirs(fsw, dir)

	fire := make(chan struct{}, 1)

	var settle *time.Timer

	arm := func() {
		if settle != nil {
			settle.Reset(h.settle)

			return
		}

		settle = time.AfterFunc(h.settle, func() {
			select {
			case fire <- struct{}{}:
			default:
			}
		})
	}

	for {
		select {
		case <-ctx.Done():
			if settle != nil {
				settle.Stop()
			}

			return

		case event, ok := <-fsw.Events:
			if !ok {
				return
			}

			// A create, a remove or a rename may have changed which directories
			// exist, and fsnotify has no recursive watch on any platform, so a new
			// images/ stays invisible until it has a watch of its own.
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				h.watchDirs(fsw, dir)
			}

			arm()

		case err, ok := <-fsw.Errors:
			if !ok {
				return
			}

			h.log.Warn("The deck file watcher reported an error", "dir", dir, "error", err)

		case <-fire:
			h.reload(dir)

			// A deck changes the theme it names by writing presentation.yaml, which
			// is a write rather than a create, so the watch set is read again after
			// every reload. Without this a deck switched onto a directory theme
			// reloaded once and then never saw that directory change again.
			h.watchDirs(fsw, dir)
		}
	}
}

// reload reads the deck and its theme again and swaps the page. Each of the
// three failures keeps the page that is already being served and says which one
// it was, since a deck that will not load mid-talk is the case the last good
// page exists for.
func (h *DeckHandler) reload(dir string) {
	deck, err := LoadDeck(dir)
	if err != nil {
		h.log.Error("Cannot load the deck, serving the page from before the change", "dir", dir, "error", err)

		return
	}

	theme, err := LoadTheme(deck.Presentation.Theme, dir)
	if err != nil {
		deck.Close()

		h.log.Error("Cannot load the theme, serving the page from before the change", "dir", dir, "theme", deck.Presentation.Theme, "error", err)

		return
	}

	err = h.Update(deck, theme)
	if err != nil {
		deck.Close()

		h.log.Error("Cannot render the deck, serving the page from before the change", "dir", dir, "error", err)

		return
	}

	h.log.Info("Reloaded the deck", "dir", dir, "slides", len(deck.Slides), "problems", len(h.Problems()))

	h.Reload()
}

// watchDirs puts a watch on the deck directory, the theme's when the theme is
// one, and every directory under each. fsnotify has no recursive watch on any
// platform, so a deck's images/ and a theme's fonts/ each need one of their own,
// and adding a path already watched is what makes this safe to call again on
// every create.
func (h *DeckHandler) watchDirs(fsw *fsnotify.Watcher, dir string) {
	dirs := []string{dir}

	h.mu.RLock()
	theme := h.theme
	h.mu.RUnlock()

	if theme != nil && theme.Dir != "" {
		dirs = append(dirs, theme.Dir)
	}

	for _, root := range dirs {
		err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if !entry.IsDir() {
				return nil
			}

			// A .git beside a deck is thousands of files nobody presents, and every
			// write in it would arm the debounce.
			if name != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}

			err = fsw.Add(name)
			if err != nil {
				h.log.Debug("Cannot watch a directory", "dir", name, "error", err)
			}

			return nil
		})
		if err != nil {
			h.log.Debug("Cannot walk a watched directory", "dir", root, "error", err)
		}
	}
}
