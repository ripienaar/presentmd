// Command presentmd serves and renders slide decks written as markdown.
//
//	presentmd serve [<flags>] <dir>
//	presentmd render <dir> <target>
//	presentmd edit [<flags>] <dir>
//
// A deck is a directory holding either presentation.md, which carries the whole
// deck, or presentation.yaml with one markdown file per slide. serve answers on
// a local port and reloads the browser as the files change; render writes the
// whole deck as one self contained HTML file that opens from disk with no
// network. edit serves a browser editor over the deck, with the deck itself
// under it, and writes what it is told to inside a root it cannot leave.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/choria-io/fisk"

	"github.com/ripienaar/presentmd/present"
)

const (
	// shutdownGrace is how long in flight requests have to finish after the
	// server is asked to stop.
	shutdownGrace = 5 * time.Second
	// headerTimeout is how long a client has to send its request headers.
	headerTimeout = 5 * time.Second
	// defaultListen is where serve binds when nothing says otherwise. The port is
	// fixed rather than picked so a browser tab left open reaches the same address
	// the next time the deck is served.
	defaultListen = "127.0.0.1:8080"
	// loopback is where a browser on this machine reaches a listener that bound
	// every interface.
	loopback = "127.0.0.1"
)

// version is reported by --version, set at build time.
var version = "development"

// command holds the flag state of both commands and the logger they write
// through.
type command struct {
	path   string
	target string
	theme  string
	listen string
	root   string
	noOpen bool
	debug  bool

	log *slog.Logger
	// level is the level of the handler the logger writes through, raised to
	// debug by --debug. The logger is built before the arguments are parsed, so
	// the flag reaches the level rather than the handler.
	level *slog.LevelVar
}

func main() {
	// The zero LevelVar is info, which is what a run without --debug logs at.
	level := &slog.LevelVar{}

	cmd := &command{
		level: level,
		log:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})),
	}

	app := newApp(cmd, cmd.serveAction, cmd.renderAction, cmd.editAction)

	app.MustParseWithUsage(os.Args[1:])
}

// newApp builds the CLI. The actions are passed in so a test parses arguments
// without starting a server.
func newApp(cmd *command, serveAction fisk.Action, renderAction fisk.Action, editAction fisk.Action) *fisk.Application {
	app := fisk.New("presentmd", "Serves and renders slide decks written as markdown")
	app.Version(version)
	app.HelpFlag.Short('h')
	app.Flag("debug", "Logs at debug level").Envar("PRESENTMD_DEBUG").UnNegatableBoolVar(&cmd.debug)

	serve := app.Command("serve", "Serves one deck over http").Default().Action(serveAction)
	serve.Arg("dir", "The deck directory to serve").Default(".").StringVar(&cmd.path)
	serve.Flag("listen", "The address to listen on as host:port, port 0 picks a free one").Envar("PRESENTMD_LISTEN").Default(defaultListen).StringVar(&cmd.listen)
	serve.Flag("no-open", "Does not open a browser on start").Envar("PRESENTMD_NO_OPEN").UnNegatableBoolVar(&cmd.noOpen)
	serve.Flag("theme", "Uses this theme instead of the one presentation.yaml names").Envar("PRESENTMD_THEME").StringVar(&cmd.theme)

	render := app.Command("render", "Writes one deck as a single self contained html file").Action(renderAction)
	render.Arg("dir", "The deck directory to render").Required().StringVar(&cmd.path)
	render.Arg("target", "The html file to write").Required().StringVar(&cmd.target)
	render.Flag("theme", "Uses this theme instead of the one presentation.yaml names").Envar("PRESENTMD_THEME").StringVar(&cmd.theme)

	edit := app.Command("edit", "Edits one deck in a browser, with the deck under it").Action(editAction)
	edit.Arg("dir", "The deck directory to edit").Default(".").StringVar(&cmd.path)
	edit.Flag("root", "Confines every read and write to this directory, the deck directory by default").Envar("PRESENTMD_ROOT").StringVar(&cmd.root)
	edit.Flag("listen", "The address to listen on as host:port, port 0 picks a free one").Envar("PRESENTMD_LISTEN").Default(defaultListen).StringVar(&cmd.listen)
	edit.Flag("no-open", "Does not open a browser on start").Envar("PRESENTMD_NO_OPEN").UnNegatableBoolVar(&cmd.noOpen)
	edit.Flag("theme", "Uses this theme instead of the one the deck names").Envar("PRESENTMD_THEME").StringVar(&cmd.theme)

	return app
}

// prepare settles the logger every command writes through: a run without
// --debug logs at info, and the level is raised here because the logger was
// built before the flag was parsed.
func (c *command) prepare() {
	if c.log == nil {
		c.log = slog.Default()
	}

	if c.debug && c.level != nil {
		c.level.Set(slog.LevelDebug)
	}
}

// themeOverride is what --theme named, empty when it was not given. A directory
// theme is made absolute here, since present resolves a relative theme path
// against the deck directory and the person typing the flag means the directory
// they typed it in.
func (c *command) themeOverride() (string, error) {
	if c.theme == "" || !present.IsDirectoryTheme(c.theme) {
		return c.theme, nil
	}

	return filepath.Abs(c.theme)
}

// themeName is the theme the run loads: the one --theme named, or the one
// presentation.yaml does.
func (c *command) themeName(deck *present.Deck) (string, error) {
	override, err := c.themeOverride()
	if err != nil {
		return "", err
	}

	if override != "" {
		return override, nil
	}

	return deck.Presentation.Theme, nil
}

func (c *command) serveAction(_ *fisk.ParseContext) error {
	c.prepare()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return c.serve(ctx)
}

func (c *command) editAction(_ *fisk.ParseContext) error {
	c.prepare()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return c.edit(ctx)
}

func (c *command) renderAction(_ *fisk.ParseContext) error {
	c.prepare()

	return c.render()
}

// render writes the deck as one self contained HTML file. A deck that loaded or
// rendered with problems writes nothing: the problems are printed and the
// command fails, since a talk with a slide missing is not one to hand to
// anyone. What the export itself could not carry, an avatar that would not
// fetch and a video background, is printed beside the file it did write.
func (c *command) render() error {
	dir, err := filepath.Abs(c.path)
	if err != nil {
		return err
	}

	deck, err := present.LoadDeck(dir)
	if err != nil {
		return err
	}
	defer deck.Close()

	name, err := c.themeName(deck)
	if err != nil {
		return err
	}

	theme, err := present.LoadTheme(name, dir)
	if err != nil {
		return err
	}

	page, problems, err := present.Export(deck, theme)

	reportProblems(problems)

	if err != nil {
		return err
	}

	err = os.WriteFile(c.target, page, 0o644)
	if err != nil {
		return err
	}

	c.log.Info("Wrote a deck", "dir", dir, "theme", theme.Name, "slides", len(deck.Slides), "target", c.target)
	fmt.Printf("Wrote %s from %s\n", c.target, dir)

	return nil
}

// serve loads the deck, binds a port for it and answers on it until ctx is
// canceled, reloading whatever the watcher reports.
func (c *command) serve(ctx context.Context) error {
	dir, err := filepath.Abs(c.path)
	if err != nil {
		return err
	}

	deck, err := present.LoadDeck(dir)
	if err != nil {
		return err
	}

	override, err := c.themeOverride()
	if err != nil {
		deck.Close()

		return err
	}

	name := override
	if name == "" {
		name = deck.Presentation.Theme
	}

	theme, err := present.LoadTheme(name, dir)
	if err != nil {
		deck.Close()

		return err
	}

	// The override is handed to the handler as well, since the watcher reads the
	// deck again on every save and would otherwise put presentation.yaml's own
	// theme back the first time a slide is edited.
	handler, err := present.Handler(deck, theme, present.HandlerOptions{
		Log:    c.log,
		Live:   true,
		Report: reportProblems,
		Theme:  override,
	})
	if err != nil {
		deck.Close()

		return err
	}
	defer handler.Close()

	listener, err := listen(c.listen, c.log)
	if err != nil {
		return err
	}

	url := listenURL(listener)

	c.log.Info("Serving a deck", "dir", dir, "theme", theme.Name, "slides", len(deck.Slides), "url", url)
	fmt.Printf("Presenting %s on %s\n", dir, url)

	if !c.noOpen {
		openBrowser(url, c.log)
	}

	go handler.Watch(ctx, dir)

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: headerTimeout,
	}

	go func() {
		<-ctx.Done()

		// The event streams end first. Shutdown waits for in flight requests and
		// an event stream is in flight for as long as its tab is open, so a deck
		// with a browser on it would otherwise sit out the whole grace period.
		handler.CloseStreams()

		timed, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()

		err := srv.Shutdown(timed)
		if err != nil {
			c.log.Warn("Shutdown did not finish within the grace period", "error", err)
		}
	}()

	err = srv.Serve(listener)
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// edit opens the editor on a deck and serves it until ctx is canceled.
//
// Every file it reads and every file it writes goes through one os.Root. The
// root is the deck directory unless --root widens it, so an editor opened on a
// talk can write that talk and nothing else, and an editor opened on a directory
// of talks can move between them and start new ones without reaching past it. A
// path that climbs out of the root, and a symlink inside it that points out of
// it, are refused by the root rather than by a check that has to be remembered.
func (c *command) edit(ctx context.Context) error {
	dir, err := filepath.Abs(c.path)
	if err != nil {
		return err
	}

	rootDir := dir
	if c.root != "" {
		rootDir, err = filepath.Abs(c.root)
		if err != nil {
			return err
		}
	}

	deck, err := filepath.Rel(rootDir, dir)
	if err != nil {
		return err
	}

	// The deck is named to the editor as a path under the root, so a deck outside
	// it is refused here where the message can say so, rather than as a file the
	// root would not open.
	if deck == ".." || strings.HasPrefix(deck, ".."+string(filepath.Separator)) {
		return fmt.Errorf("the deck %s is not below the root %s", dir, rootDir)
	}

	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer root.Close()

	override, err := c.themeOverride()
	if err != nil {
		return err
	}

	editor, err := present.NewEditor(present.EditorOptions{
		Log:   c.log,
		Root:  root,
		Deck:  filepath.ToSlash(deck),
		Theme: override,
	})
	if err != nil {
		return err
	}
	defer editor.Close()

	listener, err := listen(c.listen, c.log)
	if err != nil {
		return err
	}

	url := listenURL(listener)

	c.log.Info("Editing a deck", "dir", dir, "root", rootDir, "url", url)
	fmt.Printf("Editing %s on %s\n", dir, url)
	fmt.Printf("Reads and writes are confined to %s\n", rootDir)

	if !c.noOpen {
		openBrowser(url, c.log)
	}

	srv := &http.Server{
		Handler:           editor,
		ReadHeaderTimeout: headerTimeout,
	}

	go func() {
		<-ctx.Done()

		// The preview's event stream is in flight for as long as the editor is
		// open, so it ends before the shutdown waits on in flight requests.
		editor.CloseStreams()

		timed, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()

		err := srv.Shutdown(timed)
		if err != nil {
			c.log.Warn("Shutdown did not finish within the grace period", "error", err)
		}
	}()

	err = srv.Serve(listener)
	if err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

// listen binds addr. The default port is a fixed one so the address is the same
// from one run to the next, and a machine already using it would otherwise leave
// the command failing minutes before a talk, so an address nobody asked for
// falls back to a port the kernel picks. An address the caller named is bound or
// reported.
func listen(addr string, log *slog.Logger) (net.Listener, error) {
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		return listener, nil
	}

	if addr != defaultListen {
		return nil, err
	}

	log.Warn("The default address is taken, picking a free port", "listen", addr, "error", err)

	return net.Listen("tcp", net.JoinHostPort(loopback, "0"))
}

// listenURL is where a browser reaches a bound listener. A listener on every
// interface is reported as the loopback address, since a browser cannot open
// the unspecified one the address itself reads as.
func listenURL(listener net.Listener) string {
	addr := listener.Addr().String()

	tcp, ok := listener.Addr().(*net.TCPAddr)
	if ok && tcp.IP.IsUnspecified() {
		addr = net.JoinHostPort(loopback, strconv.Itoa(tcp.Port))
	}

	return "http://" + addr + "/"
}

// reportProblems writes what is wrong with the deck to stdout. It is the whole
// record a person presenting gets: the design puts no banner on the page.
func reportProblems(problems []present.Problem) {
	for _, problem := range problems {
		fmt.Printf("Problem in %s: %s\n", problem.Path, problem.Message)
	}
}

// openBrowser asks the desktop to open url. A failure is logged and the server
// carries on serving.
func openBrowser(url string, log *slog.Logger) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	go func() {
		err := cmd.Run()
		if err != nil {
			log.Warn("Could not open a browser", "url", url, "error", err)
		}
	}()
}
