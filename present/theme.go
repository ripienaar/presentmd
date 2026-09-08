package present

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/CloudyKit/jet/v6"
	"github.com/goccy/go-yaml"
)

// Errors returned by LoadTheme and Render. A theme either loads whole or not at
// all: unlike a deck, which holds problems against the files that carry them,
// there is nothing left to present when the theme is missing or broken.
var (
	ErrUnknownTheme  = errors.New("unknown theme")
	ErrOpenTheme     = errors.New("cannot open theme directory")
	ErrThemeConfig   = errors.New("cannot read theme.yaml")
	ErrThemeStyles   = errors.New("cannot read theme styles")
	ErrThemeTemplate = errors.New("cannot parse theme template")
	ErrNoPageStyle   = errors.New("theme has no such page style")
	ErrRenderStyle   = errors.New("cannot render page style")
	ErrRegisterTheme = errors.New("cannot register theme")
)

const (
	themeConfigFile = "theme.yaml"
	themeStylesDir  = "styles"
	embeddedDir     = "themes"
	jetExt          = ".jet"
	// partialPrefix marks a template under styles/ that is not a page style. A
	// theme needs somewhere to put the footer every style shares, and the
	// alternative to a naming rule is a second directory the loader has to know
	// about.
	partialPrefix = "_"
	// themeStyleFile is the theme's stylesheet. It is required: a theme without
	// one loads and then renders every slide unstyled, which is worse than the
	// load failing.
	themeStyleFile = "theme.css"
)

// includeRe finds the partials a template includes, which is how a dangling
// include is caught when the theme loads rather than when a slide reaches it.
var includeRe = regexp.MustCompile(`{{-?\s*include\s+"([^"]+)"`)

// themeFiles are the themes built into the binary. The all: prefix is what keeps
// a partial in the set: embed drops a file whose name opens with an underscore
// otherwise, and a theme would lose the templates its page styles include.
//
//go:embed all:themes
var themeFiles embed.FS

// registeredThemes are the themes a program importing present embedded itself
// and handed over with RegisterTheme, keyed by the name a deck writes. LoadTheme
// answers from http handlers and from the watcher, so the map is read on several
// goroutines and the lock is held for every read of it.
var (
	registeredMu     sync.RWMutex
	registeredThemes = map[string]fs.FS{}
)

// ThemeFonts are the three roles a theme's stylesheet asks for, each a CSS font
// stack rather than a single family. The page sets them as custom properties the
// stylesheet reads, so a theme that ships no font files still names what it wants
// and falls back through the stack the browser has.
type ThemeFonts struct {
	Body    string `yaml:"body,omitempty"`
	Heading string `yaml:"heading,omitempty"`
	Mono    string `yaml:"mono,omitempty"`
}

// ThemeConfig is a theme's theme.yaml. There is no base key: no reveal theme is
// vendored, so a theme's stylesheet builds on reveal.css alone and inherits
// nothing.
type ThemeConfig struct {
	// CodeStyle is the chroma style fenced code is colored with, applied once at
	// render so the export carries no highlighter.
	CodeStyle string `yaml:"code_style,omitempty"`
	// SplitStyles are the page styles whose body divides at its first thematic
	// break. Naming them here is what lets a columns slide exist without the
	// loader knowing anything about themes: in every other style a --- stays an
	// ordinary rule.
	SplitStyles []string `yaml:"split_styles,omitempty"`
	// Transition is the theme's own feel, the move reveal makes between slides
	// where the deck names none. It is checked at render rather than here: a
	// theme that will not load takes the whole deck down, and a transition
	// nobody can spell is a problem against theme.yaml with the deck still
	// rendering.
	Transition string     `yaml:"transition,omitempty"`
	Fonts      ThemeFonts `yaml:"fonts,omitempty"`
}

// Theme is a loaded theme: its configuration, its files, and every page style
// template already parsed.
type Theme struct {
	// Name is the theme as presentation.yaml wrote it, which for a directory
	// theme is the path rather than a name.
	Name string
	// Dir is the directory a directory theme was read from, and is empty for an
	// embedded one. The watcher needs it, since a theme being edited is the other
	// half of a deck being edited.
	Dir    string
	Config ThemeConfig

	fsys      fs.FS
	set       *jet.Set
	templates map[string]*jet.Template
}

// LoadTheme loads the theme presentation.yaml named. A name holding a path
// separator is a directory, resolved against deckDir when it is relative. Any
// other value is looked up among the themes RegisterTheme was given and then in
// the embedded set. There is no search path and no inheritance: a theme is in
// the binary or in one directory a person points at, and copying an embedded
// theme out of the source tree is how one starts.
//
// A directory theme may sit anywhere the process can read, including outside the
// deck and outside the planning root, and LoadTheme does not confine it. That is
// the point of the form: the user names the directory, and two decks sharing one
// theme is why they name it. Deck assets stay confined to the deck directory,
// which is the tree the binary was pointed at.
func LoadTheme(name string, deckDir string) (*Theme, error) {
	if IsDirectoryTheme(name) {
		return loadDirectoryTheme(name, deckDir)
	}

	fsys, registered := registeredTheme(name)
	if registered {
		return newTheme(name, "", fsys)
	}

	if !slices.Contains(embeddedThemes(), name) {
		return nil, fmt.Errorf("%w: %q, the themes are %s", ErrUnknownTheme, name, strings.Join(knownThemes(), ", "))
	}

	sub, err := fs.Sub(themeFiles, path.Join(embeddedDir, name))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenTheme, err)
	}

	return newTheme(name, "", sub)
}

// RegisterTheme adds a theme to the set LoadTheme resolves by name, for a program
// that imports present and carries themes of its own. The name is what a deck
// writes as its theme, and fsys is the theme itself: its root holds theme.yaml,
// theme.css and styles/, which is the shape of a theme directory.
//
// A registered theme is resolved the way an embedded one is. It names no
// directory, so the watcher does not watch it and a reload finds it again by
// name, and it holds a theme against changes to the one that ships here, since
// the program that registered it carries every file of it.
//
// The theme is built here and discarded, so one broken by an edit fails where the
// program registers it rather than the first time somebody opens a deck naming
// it. A name holding a path separator is refused, since LoadTheme reads that as a
// directory, and so is one an embedded or already registered theme answers to.
//
// Register before serving. LoadTheme takes the read side of the same lock, so a
// later call is safe rather than a race, but a deck loaded before it gets
// ErrUnknownTheme.
func RegisterTheme(name string, fsys fs.FS) error {
	if name == "" {
		return fmt.Errorf("%w: the name is empty", ErrRegisterTheme)
	}

	if IsDirectoryTheme(name) {
		return fmt.Errorf("%w: %q holds a path separator, which names a directory theme", ErrRegisterTheme, name)
	}

	if slices.Contains(embeddedThemes(), name) {
		return fmt.Errorf("%w: %q is a theme in the binary", ErrRegisterTheme, name)
	}

	_, err := newTheme(name, "", fsys)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRegisterTheme, err)
	}

	registeredMu.Lock()
	defer registeredMu.Unlock()

	_, taken := registeredThemes[name]
	if taken {
		return fmt.Errorf("%w: %q is registered", ErrRegisterTheme, name)
	}

	registeredThemes[name] = fsys

	return nil
}

// registeredTheme is the theme registered under name, if one is.
func registeredTheme(name string) (fs.FS, bool) {
	registeredMu.RLock()
	defer registeredMu.RUnlock()

	fsys, ok := registeredThemes[name]

	return fsys, ok
}

// knownThemes are every name LoadTheme answers to, registered and embedded
// together and sorted, which is what the error for a name it does not know
// lists.
func knownThemes() []string {
	names := embeddedThemes()

	registeredMu.RLock()
	defer registeredMu.RUnlock()

	for name := range registeredThemes {
		names = append(names, name)
	}

	slices.Sort(names)

	return names
}

// IsDirectoryTheme reports whether name is a path to a theme directory rather
// than the name of an embedded theme. Both separators count, so a Windows path
// in a presentation.yaml written on Windows is read as the directory it is. It
// is exported because a caller naming a theme from somewhere other than the
// deck, such as a command line flag, resolves a relative path against its own
// directory rather than the deck's.
func IsDirectoryTheme(name string) bool {
	return strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator)
}

// loadDirectoryTheme reads a theme through os.DirFS, which is the same fs.FS the
// embedded set is served through, so one loader serves both.
func loadDirectoryTheme(name string, deckDir string) (*Theme, error) {
	dir := filepath.FromSlash(name)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(deckDir, dir)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenTheme, err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrOpenTheme, dir)
	}

	return newTheme(name, dir, os.DirFS(dir))
}

// embeddedThemes are the theme directories built into the binary, sorted. It is
// what the error for an unknown name lists, since the answer to naming a theme
// that does not exist is the set that does.
func embeddedThemes() []string {
	entries, err := fs.ReadDir(themeFiles, embeddedDir)
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		names = append(names, entry.Name())
	}

	slices.Sort(names)

	return names
}

func newTheme(name string, dir string, fsys fs.FS) (*Theme, error) {
	theme := &Theme{
		Name:      name,
		Dir:       dir,
		fsys:      fsys,
		templates: map[string]*jet.Template{},
	}

	err := theme.loadConfig()
	if err != nil {
		return nil, err
	}

	err = theme.loadTemplates()
	if err != nil {
		return nil, err
	}

	for _, style := range theme.Config.SplitStyles {
		_, ok := theme.templates[style]
		if !ok {
			return nil, fmt.Errorf("%w: split_styles names %q, which has no template", ErrThemeConfig, style)
		}
	}

	return theme, nil
}

// loadConfig decodes theme.yaml strictly. A misspelled key would otherwise drop
// a font stack or a whole split style with nothing said, and theme.yaml is a
// short hand-written file whose author is the person reading the error.
func (t *Theme) loadConfig() error {
	data, err := fs.ReadFile(t.fsys, themeConfigFile)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrThemeConfig, err)
	}

	err = yaml.UnmarshalWithOptions(data, &t.Config, yaml.Strict())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrThemeConfig, err)
	}

	_, err = fs.Stat(t.fsys, themeStyleFile)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrThemeConfig, err)
	}

	return nil
}

// loadTemplates parses every template under styles/ when the theme loads,
// partials included, so a syntax error stops the theme rather than the one slide
// that first reaches it.
func (t *Theme) loadTemplates() error {
	entries, err := fs.ReadDir(t.fsys, themeStylesDir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrThemeStyles, err)
	}

	t.set = jet.NewSet(&fsLoader{fsys: t.fsys})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, jetExt) {
			continue
		}

		templatePath := path.Join(themeStylesDir, name)

		tmpl, err := t.set.GetTemplate(templatePath)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrThemeTemplate, templatePath, err)
		}

		err = t.checkIncludes(templatePath)
		if err != nil {
			return err
		}

		if strings.HasPrefix(name, partialPrefix) {
			continue
		}

		t.templates[strings.TrimSuffix(name, jetExt)] = tmpl
	}

	return nil
}

// checkIncludes reads the template source for the partials it includes and
// fails the theme when one of them is not there. Jet resolves an include when
// the template runs, so a page style naming a partial that does not exist parses
// clean and fails on the slide that first reaches it, which for a page style is
// mid-talk. Reading the source is what catches it at load, since a parsed
// template does not hand back its include targets.
func (t *Theme) checkIncludes(templatePath string) error {
	source, err := fs.ReadFile(t.fsys, templatePath)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrThemeTemplate, templatePath, err)
	}

	for _, match := range includeRe.FindAllStringSubmatch(string(source), -1) {
		target := match[1]

		// An include is written relative to the template that carries it, which
		// for a theme is a file beside it under styles/.
		if !strings.Contains(target, "/") {
			target = path.Join(themeStylesDir, target)
		}

		_, err := fs.Stat(t.fsys, strings.TrimPrefix(target, "/"))
		if err != nil {
			return fmt.Errorf("%w: %s includes %s, which is not in the theme", ErrThemeTemplate, templatePath, match[1])
		}
	}

	return nil
}

// HasStyle reports whether the theme has a template for a page style. A partial
// is not one: it is parsed and it can be included, and a slide that names it is
// a slide naming a style that does not exist.
func (t *Theme) HasStyle(style string) bool {
	_, ok := t.templates[style]

	return ok
}

// Styles are the page styles the theme offers, sorted.
func (t *Theme) Styles() []string {
	styles := make([]string, 0, len(t.templates))

	for style := range t.templates {
		styles = append(styles, style)
	}

	slices.Sort(styles)

	return styles
}

// Splits reports whether a page style's body divides at its first thematic
// break, which is what theme.yaml's split_styles names.
func (t *Theme) Splits(style string) bool {
	return slices.Contains(t.Config.SplitStyles, style)
}

// FS is the theme's own files: theme.css, the fonts and the templates. The deck
// handler serves these under the deck's /theme/ prefix and the exporter reads
// them for inlining, both from whichever of the two sources the theme came from.
func (t *Theme) FS() fs.FS {
	return t.fsys
}

// Render executes one page style template and returns the inside of a reveal
// <section>. The section element is the renderer's to write, with the style's
// class, its data-state and the slide's background attributes, so a template
// cannot forget them.
//
// A style the theme has no template for is ErrNoPageStyle, which the caller
// turns into a problem against the slide that named it.
func (t *Theme) Render(style string, data TemplateData) (string, error) {
	tmpl, ok := t.templates[style]
	if !ok {
		return "", fmt.Errorf("%w: %s has no %q", ErrNoPageStyle, t.Name, style)
	}

	var out bytes.Buffer

	err := tmpl.Execute(&out, data.vars(), nil)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %w", ErrRenderStyle, style, err)
	}

	return out.String(), nil
}

// fsLoader serves jet its templates out of an fs.FS, which is what lets the
// embedded set and a directory theme load through one code path. Jet ships a
// loader for an embed.FS and one for an http.FileSystem, and a theme is read
// through neither of those types at the point it is loaded.
type fsLoader struct {
	fsys fs.FS
}

func (l *fsLoader) Exists(templatePath string) bool {
	info, err := fs.Stat(l.fsys, fsPath(templatePath))
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func (l *fsLoader) Open(templatePath string) (io.ReadCloser, error) {
	return l.fsys.Open(fsPath(templatePath))
}

// fsPath turns the absolute slash path jet resolves a template to into an fs.FS
// path, which is relative and never opens with a slash. Jet builds those paths
// itself when it resolves an include against the template that wrote it.
func fsPath(templatePath string) string {
	return strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(templatePath)), "/")
}
