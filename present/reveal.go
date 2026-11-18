package present

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

// revealVersion is the reveal.js release vendored under reveal/. The version is
// pinned here and asserted against the vendored reveal.js, so an upgrade that
// copies some files and not others fails a test rather than a deck.
const revealVersion = "6.0.1"

// revealFiles is the subset of reveal's dist a deck page needs: the script, the
// core stylesheets and the speaker notes plugin, which carries the speaker view
// HTML inside it.
//
// No reveal theme is vendored. Reveal's own themes inline several hundred
// kilobytes of font faces a deck may never use, and they open with an @import of
// a font stylesheet that is not part of this subset, which would fail to load
// when served and be missing from an export. A planboard theme's stylesheet
// builds on reveal.css alone and sets its own type.
//
// The markdown and highlight plugins are not vendored either: goldmark renders
// the markdown and chroma colors the code before the page is ever assembled.
//
//go:embed reveal/reveal.js reveal/reveal.css reveal/reset.css reveal/plugin/notes.js reveal/LICENSE
var revealFiles embed.FS

// RevealFS is reveal's vendored files rooted at reveal.js rather than at the
// reveal directory, so a caller asks for "reveal.js" and "plugin/notes.js". The
// deck handler serves these and the exporter reads them for inlining, both from
// this one copy.
func RevealFS() fs.FS {
	sub, err := fs.Sub(revealFiles, "reveal")
	if err != nil {
		panic(fmt.Sprintf("vendored reveal files are not readable: %v", err))
	}

	return sub
}

// RevealVersion is the vendored reveal.js release.
func RevealVersion() string {
	return revealVersion
}

const (
	// speakerScriptOpen and speakerScriptClose bound the speaker view's inline
	// script inside the template literal notes.js holds the speaker view in. The
	// close is written escaped there, which the literal cooks back to </script>.
	speakerScriptOpen  = "<script>"
	speakerScriptClose = `<\/script>`
)

// speakerViewHash is the speaker view's script hash, worked out from the
// vendored notes.js the first time it is asked for.
var speakerViewHash = sync.OnceValues(func() (string, error) {
	notes, err := fs.ReadFile(revealFiles, "reveal/"+revealNotesPlug)
	if err != nil {
		return "", err
	}

	script, err := speakerViewScript(string(notes))
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(script))

	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'", nil
})

// SpeakerViewScriptHash is the hash of the inline script in reveal's speaker
// view, quoted as a Content-Security-Policy source: 'sha256-...'. The notes
// plugin opens the speaker view as about:blank and writes it with
// document.write, which leaves that window under the policy of the page that
// opened it, so a host allowing presentmd's scripts by nonce adds this to its
// script-src for the speaker view to run.
//
// The hash is worked out from the vendored notes.js, which is embedded, so the
// answer is fixed at build time and a test asserts it can be worked out. It
// panics when it cannot: a reveal upgrade that moves the speaker view's script
// fails that test rather than reaching a host.
func SpeakerViewScriptHash() string {
	hash, err := speakerViewHash()
	if err != nil {
		panic(fmt.Sprintf("cannot hash the speaker view script: %v", err))
	}

	return hash
}

// speakerViewScript is the speaker view's inline script as the browser runs it
// once the plugin has written the view: the text between the template literal's
// one <script> and the first <\/script> after it, with the literal's escapes
// cooked the way JavaScript cooks them.
//
// Only an escaped backslash is cooked. Any other escape, a substitution, a CR
// (which a template literal turns into a LF) or a byte outside ASCII is an error
// rather than a guess, since a hash of text the browser does not run allows
// nothing and fails without a word.
func speakerViewScript(notes string) (string, error) {
	start := strings.Index(notes, speakerScriptOpen)
	if start < 0 {
		return "", fmt.Errorf("notes.js holds no %s", speakerScriptOpen)
	}

	// The literal the script is in runs from the backtick before it to the
	// backtick after it, and holds no other script.
	literalStart := strings.LastIndexByte(notes[:start], '`')
	literalEnd := strings.IndexByte(notes[start:], '`')

	if literalStart < 0 || literalEnd < 0 {
		return "", fmt.Errorf("the speaker view's %s is not inside a template literal", speakerScriptOpen)
	}

	literal := notes[literalStart+1 : start+literalEnd]

	if count := strings.Count(literal, "<script"); count != 1 {
		return "", fmt.Errorf("the speaker view holds %d script elements, want 1", count)
	}

	body := notes[start+len(speakerScriptOpen) : start+literalEnd]

	end := strings.Index(body, speakerScriptClose)
	if end < 0 {
		return "", fmt.Errorf("the speaker view's script has no %s", speakerScriptClose)
	}

	return cookTemplateText(body[:end])
}

// cookTemplateText is raw template literal text as JavaScript reads it, for text
// whose only escape is an escaped backslash.
func cookTemplateText(raw string) (string, error) {
	if strings.Contains(raw, "${") {
		return "", errors.New("the speaker view's script holds a substitution")
	}

	var out strings.Builder

	for i := 0; i < len(raw); i++ {
		c := raw[i]

		switch {
		case c == '`':
			return "", errors.New("the speaker view's script holds a backtick")
		case c == '\r':
			return "", errors.New("the speaker view's script holds a carriage return")
		case c > 0x7f:
			return "", fmt.Errorf("the speaker view's script holds a byte outside ASCII at %d", i)
		case c == '\\':
			if i+1 >= len(raw) || raw[i+1] != '\\' {
				return "", fmt.Errorf("the speaker view's script holds an escape other than \\\\ at %d", i)
			}

			out.WriteByte('\\')
			i++
		default:
			out.WriteByte(c)
		}
	}

	return out.String(), nil
}
