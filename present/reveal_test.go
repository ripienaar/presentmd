package present

import (
	"io/fs"
	"strings"
	"testing"
)

func TestRevealFS(t *testing.T) {
	rfs := RevealFS()

	// Every file the page template references. A copy that missed one would
	// serve a deck with no script or no speaker view, which is only visible when
	// someone presents.
	want := []string{
		"reveal.js",
		"reveal.css",
		"reset.css",
		"plugin/notes.js",
		"LICENSE",
	}

	for _, name := range want {
		info, err := fs.Stat(rfs, name)
		if err != nil {
			t.Fatalf("%s is not embedded: %v", name, err)
		}

		if info.Size() == 0 {
			t.Fatalf("%s is embedded but empty", name)
		}
	}
}

func TestRevealVersionMatchesVendoredFiles(t *testing.T) {
	body, err := fs.ReadFile(RevealFS(), "reveal.js")
	if err != nil {
		t.Fatalf("cannot read the vendored reveal.js: %v", err)
	}

	if !strings.Contains(string(body), RevealVersion()) {
		t.Fatalf("vendored reveal.js does not carry version %s", RevealVersion())
	}
}

func TestNoRevealThemeIsVendored(t *testing.T) {
	// A vendored reveal theme would import a font stylesheet this subset does not
	// carry, so the absence is asserted rather than left to a reviewer.
	_, err := fs.Stat(RevealFS(), "theme")
	if err == nil {
		t.Fatal("a reveal theme directory is vendored, which theme.css is written to replace")
	}
}

func TestNoUnusedPluginsAreVendored(t *testing.T) {
	entries, err := fs.ReadDir(RevealFS(), "plugin")
	if err != nil {
		t.Fatalf("cannot read the plugin directory: %v", err)
	}

	if len(entries) != 1 || entries[0].Name() != "notes.js" {
		t.Fatalf("expected only notes.js under plugin, got %d entries", len(entries))
	}
}
