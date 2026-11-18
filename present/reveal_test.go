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

// speakerViewHash601 is the speaker view's script hash for reveal 6.0.1. A
// reveal upgrade changes it, and so does a change to how the script is read out
// of notes.js, which is the one a reviewer has to look at.
const speakerViewHash601 = "'sha256-GzCveToXhSIzS3M5eQeRm3McVRB7cYneYKwTkzA3wDk='"

func TestSpeakerViewScriptHash(t *testing.T) {
	hash, err := speakerViewHash()
	if err != nil {
		t.Fatalf("the speaker view script cannot be hashed: %v", err)
	}

	if hash != SpeakerViewScriptHash() {
		t.Fatalf("SpeakerViewScriptHash is %s, the helper %s", SpeakerViewScriptHash(), hash)
	}

	if RevealVersion() == "6.0.1" && hash != speakerViewHash601 {
		t.Fatalf("the speaker view script hashes to %s, want %s", hash, speakerViewHash601)
	}
}

func TestSpeakerViewScriptIsTheOneInNotes(t *testing.T) {
	notes, err := fs.ReadFile(RevealFS(), "plugin/notes.js")
	if err != nil {
		t.Fatalf("cannot read the vendored notes.js: %v", err)
	}

	if count := strings.Count(string(notes), "<script"); count != 1 {
		t.Fatalf("notes.js holds <script %d times, want 1", count)
	}

	if count := strings.Count(string(notes), speakerScriptClose); count != 1 {
		t.Fatalf("notes.js holds %s %d times, want 1", speakerScriptClose, count)
	}

	script, err := speakerViewScript(string(notes))
	if err != nil {
		t.Fatalf("the speaker view script cannot be read: %v", err)
	}

	if !strings.HasPrefix(script, "\n\n\t\t\t(function() {\n") {
		t.Errorf("the speaker view script does not open with its leading lines and function: %q", script[:min(len(script), 40)])
	}

	if !strings.HasSuffix(script, "})();\n\n\t\t") {
		t.Errorf("the speaker view script does not end where its function does: %q", script[max(0, len(script)-40):])
	}

	// The one escape in 6.0.1 is a regexp's \\?, which the browser runs as \?.
	if !strings.Contains(script, `/\?/.test(data.url)`) || strings.Contains(script, `\\`) {
		t.Error("the speaker view script's escaped backslash was not cooked")
	}
}

func TestSpeakerViewScriptRefusesWhatItCannotCook(t *testing.T) {
	cases := map[string]string{
		"no script":     "var e=`<html></html>`;",
		"no close":      "var e=`<html><script>run()</script></html>`;",
		"two scripts":   "var e=`<script>a()<\\/script><script src=x><\\/script>`;",
		"substitution":  "var e=`<script>${a}<\\/script>`;",
		"other escape":  "var e=`<script>a('\\n')<\\/script>`;",
		"lone escape":   "var e=`<script>a\\<\\/script>`;",
		"carriage":      "var e=`<script>a()\r\n<\\/script>`;",
		"outside ascii": "var e=`<script>a('" + string(rune(0xe9)) + "')<\\/script>`;",
		"no literal":    "<script>a()<\\/script>",
	}

	for name, notes := range cases {
		_, err := speakerViewScript(notes)
		if err == nil {
			t.Errorf("%s: the script was read", name)
		}
	}

	script, err := speakerViewScript("x=`a`;var e=`<p><script>\na(/\\\\?/)\n<\\/script></p>`;y=`b`")
	if err != nil {
		t.Fatalf("a script with an escaped backslash was refused: %v", err)
	}

	if script != "\na(/\\?/)\n" {
		t.Errorf("the script read as %q", script)
	}
}
