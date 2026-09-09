package present

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// slideMarkdown renders one slide's markdown. It holds the goldmark the whole
// deck is rendered through, built once for the deck because building it walks
// every extension and a deck renders on every request under the board.
type slideMarkdown struct {
	md goldmark.Markdown
	// chromaFormatter writes the stylesheet the highlighted code reads its
	// classes from. It carries the same options the fenced blocks were formatted
	// with, since the line number rules only reach the sheet when the formatter
	// that writes it was asked for line numbers too.
	chromaFormatter *chromahtml.Formatter
	codeStyle       string
	// slidePrefix is set before each slide renders and is what keeps the ids of
	// one slide from colliding with another's on the single page the deck
	// becomes. The renderer works one slide at a time, so a field carries it
	// rather than threading it through every call; a slideMarkdown is not for
	// concurrent use.
	slidePrefix string
	// tints are what the slide being rendered got wrong about a tint: a role
	// nobody defined, or one opened and never closed. The markdown of a slide is
	// rendered through four calls, the body, the caption, the call to action and
	// the notes, so the parser writes here and the renderer reads it once the
	// slide is done rather than each call carrying its own list back.
	tints []string
}

// resetTints empties the problems the tints of the last slide raised. The
// renderer calls it where it sets slidePrefix, so what tintProblems answers with
// is one slide's worth.
func (m *slideMarkdown) resetTints() {
	m.tints = nil
}

// tintProblems is what the slide's tints got wrong, in the order they were
// written.
func (m *slideMarkdown) tintProblems() []string {
	return m.tints
}

// prefixedIDs generates the ids goldmark writes for headings, putting the
// current slide in front of each so two slides sharing a heading do not share
// an id on the page they are rendered into together. goldmark keeps its own
// implementation unexported, so this is the whole of one: slugify the text,
// prefix it, and count a repeat within the slide the way goldmark does.
type prefixedIDs struct {
	prefix string
	used   map[string]bool
}

func newPrefixedIDs(prefix string) *prefixedIDs {
	return &prefixedIDs{prefix: prefix, used: map[string]bool{}}
}

func (p *prefixedIDs) Generate(value []byte, _ ast.NodeKind) []byte {
	slug := slugify(string(value))
	if slug == "" {
		slug = "heading"
	}

	candidate := p.prefix + slug

	for i := 1; p.used[candidate]; i++ {
		candidate = fmt.Sprintf("%s%s-%d", p.prefix, slug, i)
	}

	p.used[candidate] = true

	return []byte(candidate)
}

func (p *prefixedIDs) Put(value []byte) {
	p.used[string(value)] = true
}

// slugify turns heading text into the id part of an anchor: lowercase, with
// every run that is not a letter or a digit becoming one hyphen.
func slugify(value string) string {
	var out strings.Builder

	lastHyphen := false

	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			lastHyphen = false

			continue
		}

		if !lastHyphen && out.Len() > 0 {
			out.WriteByte('-')
			lastHyphen = true
		}
	}

	return strings.TrimRight(out.String(), "-")
}

// newSlideMarkdown builds the package's own goldmark. It is the board's set of
// extensions less the wiki-link one, so a [[reference]] on a slide is left as
// the text it was written as: a deck is not part of the planning tree and has
// nothing to resolve a target against.
//
// Raw HTML passes through as it does in a board overview, so a hand-authored
// SVG on a slide is the drawing it was written to be rather than a page of
// angle brackets. The same reasoning applies: every slide rendered here is one
// the reader or their agent wrote on their own disk.
//
// Fenced code goes through chroma with the theme's code style and line numbers
// on every block, emitting CSS classes rather than inline styles. No CSS writer
// is set: goldmark-highlighting writes the stylesheet once per block, and the
// page wants it once.
func newSlideMarkdown(codeStyle string) *slideMarkdown {
	formatOptions := []chromahtml.Option{
		chromahtml.WithClasses(true),
		chromahtml.WithLineNumbers(true),
	}

	m := &slideMarkdown{
		codeStyle:       codeStyle,
		chromaFormatter: chromahtml.New(formatOptions...),
	}

	// The footnote ids carry the slide in front of them for the reason the
	// heading ids do: one page holds every slide, and without a prefix the second
	// slide's footnote link jumps to the first slide's note.
	footnotes := extension.NewFootnote(
		extension.WithFootnoteIDPrefixFunction(func(ast.Node) []byte {
			return []byte(m.slidePrefix)
		}),
	)

	// The markdown between a tint's tags is parsed by a goldmark of its own, which
	// carries the inline extensions a run of words can use and not the tint, so a
	// tint cannot open inside a tint. The block extensions are left out: what sits
	// between the tags is part of a line rather than a document of its own.
	tints := &tintExtension{
		inner: goldmark.New(
			goldmark.WithExtensions(extension.Strikethrough, extension.Linkify),
			goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
		),
		report: func(problem string) { m.tints = append(m.tints, problem) },
	}

	m.md = goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			tints,
			footnotes,
			highlighting.NewHighlighting(
				highlighting.WithStyle(codeStyle),
				highlighting.WithFormatOptions(formatOptions...),
				// Guessing is on for the fallback it carries rather than the guess: a
				// fence with no language has no lexer, and without this the block
				// renders as a bare pre with none of the frame or the line numbers the
				// theme's code style is written against. The extension falls back to a
				// text lexer when the guess fails, which is what a terminal transcript
				// on a slide wants.
				highlighting.WithGuessLanguage(true),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)

	return m
}

// stylesheet is chroma's CSS for the theme's code style, written once for the
// whole page. A style name chroma does not know falls back to chroma's own,
// which colors the code rather than leaving a deck with none.
func (m *slideMarkdown) stylesheet() (string, error) {
	var out bytes.Buffer

	err := m.chromaFormatter.WriteCSS(&out, styles.Get(m.codeStyle))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrCodeStylesheet, err)
	}

	return out.String(), nil
}

// slideBody is one slide's markdown after the parts a template places
// separately have been taken out of it.
type slideBody struct {
	// Heading is the slide's first level-one heading rendered on its own,
	// without the h1 the theme writes around it, and is empty for a slide that
	// opened with none.
	Heading string
	Body    string
	// Left and Right are the halves of a split style's body, and are empty for
	// every other style.
	Left  string
	Right string
}

// render turns a slide's markdown into the heading and the body a template is
// executed with. The document is parsed once: lifting the heading and splitting
// the halves are both edits to that one tree rather than second passes over the
// text.
//
// splits is the theme's answer for this slide's page style. When it is set the
// body divides at its first thematic break and a second break is an error, and
// when it is not every --- stays an ordinary rule.
func (m *slideMarkdown) render(markdown string, splits bool) (slideBody, error) {
	source := []byte(markdown)

	// Every slide of a deck lands in one HTML page, so the ids goldmark generates
	// have to be unique across the deck rather than within the slide. Parsing each
	// slide with a fresh context of its own gave two slides holding "## Setup"
	// one "setup" each, and two slides each using a footnote one "fn:1" each,
	// where the second slide's footnote link jumps to the first slide's note.
	// The prefix is the slide's number, which is unique by the time the loader is
	// done with it.
	context := parser.NewContext(parser.WithIDs(newPrefixedIDs(m.slidePrefix)))

	document := m.md.Parser().Parse(text.NewReader(source), parser.WithContext(context))

	var out slideBody

	heading := liftHeading(document)
	if heading != nil {
		rendered, err := m.renderChildren(heading, source)
		if err != nil {
			return out, err
		}

		out.Heading = rendered
	}

	if !splits {
		body, err := m.renderNode(document, source)
		if err != nil {
			return out, err
		}

		out.Body = body

		return out, nil
	}

	left, right, ok := splitAtBreak(document)
	if !ok {
		// A split style with no break is the author writing one column, which the
		// template places on the left and leaves the right of empty.
		left = document
		right = ast.NewDocument()
	}

	if hasBreak(right) {
		return out, ErrSecondBreak
	}

	rendered, err := m.renderNode(left, source)
	if err != nil {
		return out, err
	}

	out.Left = rendered

	rendered, err = m.renderNode(right, source)
	if err != nil {
		return out, err
	}

	out.Right = rendered

	return out, nil
}

// renderMarkdown renders a frontmatter string as ordinary block markdown, which
// is what the speaker notes are: a paragraph or a list the presenter reads, not
// a line placed inside a theme's element.
func (m *slideMarkdown) renderMarkdown(markdown string) (string, error) {
	if markdown == "" {
		return "", nil
	}

	source := []byte(markdown)

	return m.renderNode(m.md.Parser().Parse(text.NewReader(source)), source)
}

// renderInline renders a frontmatter string as markdown without the paragraph
// goldmark wraps a line of prose in. A caption and a call to action are placed
// inside the element a theme drew for them, and a <p> inside that element is a
// second box the theme never styled.
func (m *slideMarkdown) renderInline(markdown string) (string, error) {
	if markdown == "" {
		return "", nil
	}

	source := []byte(markdown)
	document := m.md.Parser().Parse(text.NewReader(source))

	for child := document.FirstChild(); child != nil; {
		next := child.NextSibling()

		paragraph, ok := child.(*ast.Paragraph)
		if ok {
			block := ast.NewTextBlock()
			moveChildren(block, paragraph)
			document.ReplaceChild(document, paragraph, block)
		}

		child = next
	}

	rendered, err := m.renderNode(document, source)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(rendered), nil
}

// renderChildren renders a node's children without the element that held them,
// which is how the lifted heading becomes the string a theme places inside its
// own h1. The children move into a text block because the HTML renderer writes
// nothing of its own for one.
func (m *slideMarkdown) renderChildren(node ast.Node, source []byte) (string, error) {
	document := ast.NewDocument()
	block := ast.NewTextBlock()

	document.AppendChild(document, block)
	moveChildren(block, node)

	rendered, err := m.renderNode(document, source)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(rendered), nil
}

func (m *slideMarkdown) renderNode(node ast.Node, source []byte) (string, error) {
	var out bytes.Buffer

	err := m.md.Renderer().Render(&out, source, node)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrRenderMarkdown, err)
	}

	return out.String(), nil
}

// liftHeading takes the first level-one heading out of a document and returns
// it, which is the interview's rule that a slide's own H1 is the heading the
// theme places rather than a heading inside the body. A deeper heading stays
// where it was written.
func liftHeading(document ast.Node) ast.Node {
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		heading, ok := child.(*ast.Heading)
		if !ok || heading.Level != 1 {
			continue
		}

		document.RemoveChild(document, heading)

		return heading
	}

	return nil
}

// splitAtBreak divides a document at its first top level thematic break into
// the two halves a split style places side by side. The break itself is dropped:
// it is the divider the author wrote, not a rule they want drawn.
func splitAtBreak(document ast.Node) (ast.Node, ast.Node, bool) {
	var found ast.Node

	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		_, ok := child.(*ast.ThematicBreak)
		if ok {
			found = child

			break
		}
	}

	if found == nil {
		return nil, nil, false
	}

	right := ast.NewDocument()

	for child := found.NextSibling(); child != nil; {
		next := child.NextSibling()

		document.RemoveChild(document, child)
		right.AppendChild(right, child)

		child = next
	}

	document.RemoveChild(document, found)

	return document, right, true
}

// hasBreak reports whether a document still holds a top level thematic break,
// which in a split style is a second divider the two halves have no room for.
func hasBreak(document ast.Node) bool {
	for child := document.FirstChild(); child != nil; child = child.NextSibling() {
		_, ok := child.(*ast.ThematicBreak)
		if ok {
			return true
		}
	}

	return false
}

// moveChildren reparents every child of src onto dst. The nodes keep pointing
// at the same source, so the text segments they hold still read the slide they
// were parsed from.
func moveChildren(dst ast.Node, src ast.Node) {
	for {
		child := src.FirstChild()
		if child == nil {
			return
		}

		src.RemoveChild(src, child)
		dst.AppendChild(dst, child)
	}
}
