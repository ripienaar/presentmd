package present

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
)

// EncodeDeck writes a presentation and its slides as one presentation.md: the
// presentation's yaml as the file's frontmatter, then, for each slide in turn, a
// break, that slide's yaml, another break and its markdown.
//
// It is the whole write path of the editor, and the editor renders what it
// returns rather than what the browser holds: the bytes are read back through
// the same loader a deck on disk goes through, so the preview is the file a save
// would write rather than a second rendering of the same intent.
//
// A slide whose body carries a slide break of its own outside a fenced code
// block is refused. Written out it would open a slide nobody wrote, and the deck
// read back would not be the deck that was handed over.
func EncodeDeck(presentation Presentation, slides []*Slide) ([]byte, error) {
	front, err := marshalFront(presentation)
	if err != nil {
		return nil, fmt.Errorf("cannot write the presentation: %w", err)
	}

	out := &bytes.Buffer{}

	out.WriteString(fence + "\n")
	out.Write(front)
	out.WriteString(fence + "\n")

	for i, slide := range slides {
		body := strings.TrimSpace(slide.Markdown)

		if carriesBreak(body) {
			return nil, fmt.Errorf("slide %d writes a %s of its own outside a code fence, which would open a slide", i+1, slideBreak)
		}

		front, err := marshalFront(slideFront{
			PageStyle:  slide.PageStyle,
			Caption:    slide.Caption,
			CTA:        slide.CTA,
			Notes:      slide.Notes,
			Background: slide.Background,
			Transition: slide.Transition,
			TextSize:   slide.TextSize,
		})
		if err != nil {
			return nil, fmt.Errorf("cannot write slide %d: %w", i+1, err)
		}

		out.WriteString("\n" + slideBreak + "\n")
		out.Write(front)
		out.WriteString(slideBreak + "\n")

		// A slide with no body is written as its two breaks with nothing between
		// them, which is what a title slide carrying only its page style is.
		if body != "" {
			out.WriteString("\n" + body + "\n")
		}
	}

	return out.Bytes(), nil
}

// marshalFront writes one frontmatter block. A value over several lines is
// written as a block scalar rather than as one escaped line, since a slide's
// notes are prose that someone also reads in a text editor. A block with nothing
// set is written as nothing: yaml's empty mapping between two breaks reads as a
// slide with no page_style either way, and the file says so more quietly.
func marshalFront(value any) ([]byte, error) {
	data, err := yaml.MarshalWithOptions(value, yaml.UseLiteralStyleIfMultiline(true))
	if err != nil {
		return nil, err
	}

	if bytes.Equal(bytes.TrimSpace(data), []byte("{}")) {
		return nil, nil
	}

	return data, nil
}

// carriesBreak says whether a slide body holds a slide break of its own. The
// scanner is the loader's, so a break inside a fenced code block counts as the
// deck's own text here exactly as it does when the file is read back.
func carriesBreak(body string) bool {
	return len(splitBreaks(body, 1)) > 1
}
