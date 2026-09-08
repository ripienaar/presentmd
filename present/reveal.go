package present

import (
	"embed"
	"fmt"
	"io/fs"
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
