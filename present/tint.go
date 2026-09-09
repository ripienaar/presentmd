package present

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// tintRoles are the names a slide colours a run of words with. They are roles
// rather than colours because a deck outlives the theme it was written under: a
// slide that says good still means good on the next theme, where one that said
// green would mean whatever green the next theme happens to have.
var tintRoles = []string{"accent", "muted", "good", "bad"}

// tintClose closes whichever role is open. It carries no name because there is
// only ever one open: the first close ends the tint, and a tint inside a tint is
// not a thing a slide needs.
const tintClose = "{{/}}"

// tintOpenRe matches an opening tag. The name is an identifier rather than
// anything a slide might write between braces, so the templating a deck about
// jet or Go templates puts in its prose is not mistaken for a tint.
var tintOpenRe = regexp.MustCompile(`^\{\{([A-Za-z][A-Za-z0-9-]*)\}\}`)

// tintKind is this package's own inline node, the run of words a role colours.
var tintKind = ast.NewNodeKind("Tint")

type tintNode struct {
	ast.BaseInline

	role string
}

func (n *tintNode) Kind() ast.NodeKind {
	return tintKind
}

func (n *tintNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"role": n.role}, nil)
}

// tintExtension is the goldmark extension the deck's markdown carries. It is
// built per deck rather than once for the package because report writes the
// problems back to the slide being rendered.
type tintExtension struct {
	// inner parses what is between the tags. It is a goldmark of its own without
	// this extension in it, which is what keeps a tint from opening inside a tint
	// and is why the markdown between the tags is still markdown.
	inner goldmark.Markdown
	// report is handed the problems a slide's tints raise, which is how a name
	// nobody defined reaches the reader rather than sitting silently on a slide.
	report func(string)
}

func (e *tintExtension) Extend(md goldmark.Markdown) {
	md.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&tintParser{inner: e.inner, report: e.report}, 500),
	))

	md.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&tintRenderer{}, 500),
	))
}

type tintParser struct {
	inner  goldmark.Markdown
	report func(string)
}

// Trigger is the brace an opening tag starts with. Nothing else in the deck's
// markdown parses one, so a line without a tint costs the match and nothing
// more.
func (p *tintParser) Trigger() []byte {
	return []byte{'{'}
}

// Parse reads one tint. Everything it does not recognise is left as the text it
// was written as: returning nil restores the reader to the brace it was standing
// on, and goldmark merges the text back as though this parser had never run. A
// slide that means the braces writes \{{ and this never sees them.
//
// The close is looked for across the rest of the block rather than the rest of
// the line, since prose on a slide soft wraps and a tint that ended one line
// down would otherwise be the common case rather than the exception.
func (p *tintParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()

	match := tintOpenRe.FindSubmatch(line)
	if match == nil {
		return nil
	}

	role := string(match[1])

	if !slices.Contains(tintRoles, role) {
		p.report(fmt.Sprintf("{{%s}} is not one of %s, so it was left as written", role, strings.Join(tintRoles, ", ")))

		return nil
	}

	openLine, openPos := block.Position()

	block.Advance(len(match[0]))

	segments := text.NewSegments()

	for {
		line, segment := block.PeekLine()
		if line == nil {
			block.SetPosition(openLine, openPos)
			p.report(fmt.Sprintf("{{%s}} was opened and never closed with %s, so it was left as written", role, tintClose))

			return nil
		}

		at := bytes.Index(line, []byte(tintClose))
		if at < 0 {
			segments.Append(segment)
			block.AdvanceLine()

			continue
		}

		// A line the block reader pads carries that padding in front of the value
		// the index was found in, so the padding comes off the offset before it is
		// a position in the source.
		segments.Append(segment.WithStop(segment.Start + at - segment.Padding))
		block.Advance(at + len(tintClose))

		break
	}

	node := &tintNode{role: role}

	// The segments point into the source the outer parse is reading, so the nodes
	// this returns render against that same source and the renderer needs to know
	// nothing about where they came from.
	inner := p.inner.Parser().Parse(text.NewBlockReader(block.Source(), segments))

	for child := inner.FirstChild(); child != nil; {
		next := child.NextSibling()

		moveChildren(node, child)

		child = next
	}

	return node
}

type tintRenderer struct{}

func (r *tintRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(tintKind, r.render)
}

// render writes the span a theme colours. The class carries the role as the
// parser matched it against tintRoles, so nothing a slide wrote reaches the
// attribute.
func (r *tintRenderer) render(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = w.WriteString("</span>")

		return ast.WalkContinue, nil
	}

	tint, ok := node.(*tintNode)
	if !ok {
		return ast.WalkContinue, nil
	}

	_, _ = w.WriteString(`<span class="tint tint-` + tint.role + `">`)

	return ast.WalkContinue, nil
}
