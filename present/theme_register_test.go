package present

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// registeredFiles is the smallest thing a program would hand RegisterTheme: the
// three parts every theme has, with one page style so a loaded theme has
// something to answer HasStyle with.
func registeredFiles() fstest.MapFS {
	return fstest.MapFS{
		themeConfigFile:      &fstest.MapFile{Data: []byte("code_style: bw\n")},
		themeStyleFile:       &fstest.MapFile{Data: []byte(".reveal { color: black; }\n")},
		"styles/content.jet": &fstest.MapFile{Data: []byte("<section>{{ raw(Body) }}</section>\n")},
	}
}

// register registers a theme and gives the name back at the end of the test. The
// registry belongs to the package rather than to a test, so a name left behind
// is a name the next test cannot use.
func register(t *testing.T, name string, files fstest.MapFS) error {
	t.Helper()

	t.Cleanup(func() {
		registeredMu.Lock()
		defer registeredMu.Unlock()

		delete(registeredThemes, name)
	})

	return RegisterTheme(name, files)
}

// TestRegisterThemeIsLoadedByName pins what registration is for: the deck writes
// a name, and LoadTheme resolves it out of the program rather than out of the
// binary's own set.
func TestRegisterThemeIsLoadedByName(t *testing.T) {
	err := register(t, "board", registeredFiles())
	if err != nil {
		t.Fatalf("cannot register a theme: %v", err)
	}

	theme, err := LoadTheme("board", deckDir)
	if err != nil {
		t.Fatalf("cannot load the registered theme: %v", err)
	}

	if theme.Name != "board" || theme.Dir != "" {
		t.Errorf("name %q dir %q, want a registered theme to name no directory", theme.Name, theme.Dir)
	}

	if !theme.HasStyle("content") {
		t.Errorf("styles %v, want content", theme.Styles())
	}

	if theme.Config.CodeStyle != "bw" {
		t.Errorf("code style %q, want the registered theme.yaml decoded", theme.Config.CodeStyle)
	}
}

// TestRegisterThemeRefusesATakenName pins that registration cannot replace a
// theme, its own or the binary's. A program registering over a name is one whose
// decks would render as something other than what they name.
func TestRegisterThemeRefusesATakenName(t *testing.T) {
	err := register(t, "board", registeredFiles())
	if err != nil {
		t.Fatalf("cannot register a theme: %v", err)
	}

	err = RegisterTheme("board", registeredFiles())
	if !errors.Is(err, ErrRegisterTheme) {
		t.Errorf("registering a name twice answered %v, want ErrRegisterTheme", err)
	}

	err = register(t, "default", registeredFiles())
	if !errors.Is(err, ErrRegisterTheme) {
		t.Errorf("registering over the embedded theme answered %v, want ErrRegisterTheme", err)
	}
}

// TestRegisterThemeRefusesAPathName pins the rule that keeps the two forms
// apart. A registered name holding a separator would never be reached, since
// LoadTheme reads that as the directory theme it looks like.
func TestRegisterThemeRefusesAPathName(t *testing.T) {
	for _, name := range []string{"./board", "themes/board", ""} {
		err := register(t, name, registeredFiles())
		if !errors.Is(err, ErrRegisterTheme) {
			t.Errorf("registering %q answered %v, want ErrRegisterTheme", name, err)
		}
	}
}

// TestRegisterThemeRefusesABrokenTheme pins where a theme broken by an edit is
// reported: at the program that carries it, rather than at the first person to
// open a deck naming it.
func TestRegisterThemeRefusesABrokenTheme(t *testing.T) {
	files := registeredFiles()
	delete(files, themeStyleFile)

	err := register(t, "board", files)
	if !errors.Is(err, ErrRegisterTheme) {
		t.Fatalf("registering a theme with no stylesheet answered %v, want ErrRegisterTheme", err)
	}

	_, err = LoadTheme("board", deckDir)
	if !errors.Is(err, ErrUnknownTheme) {
		t.Errorf("a theme that failed to register loaded with %v, want ErrUnknownTheme", err)
	}
}

// TestUnknownThemeNamesRegisteredThemes pins the error a person reads when they
// misspell a theme: it lists what the binary they are running answers to, which
// is both sets rather than the built in one.
func TestUnknownThemeNamesRegisteredThemes(t *testing.T) {
	err := register(t, "board", registeredFiles())
	if err != nil {
		t.Fatalf("cannot register a theme: %v", err)
	}

	_, err = LoadTheme("fancy", deckDir)
	if !errors.Is(err, ErrUnknownTheme) {
		t.Fatalf("error was %v, want ErrUnknownTheme", err)
	}

	for _, want := range []string{"board", "default"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name the %s theme", err, want)
		}
	}
}
