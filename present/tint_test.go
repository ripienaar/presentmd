package present

import (
	"strings"
	"testing"
)

// TestTintRenders pins what a tint becomes and, as much as the cases it carries,
// what is left alone. A slide's prose is the one place a deck writes braces for
// its own reasons, so everything this does not recognise has to come out as the
// text it was written as.
func TestTintRenders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		markdown string
		want     string
		problems int
	}{
		{
			name:     "a role colours a run of words",
			markdown: "The {{good}}fast{{/}} path",
			want:     `The <span class="tint tint-good">fast</span> path`,
		},
		{
			name:     "every role",
			markdown: "{{accent}}a{{/}} {{muted}}b{{/}} {{good}}c{{/}} {{bad}}d{{/}}",
			want:     `<span class="tint tint-accent">a</span> <span class="tint tint-muted">b</span> <span class="tint tint-good">c</span> <span class="tint tint-bad">d</span>`,
		},
		{
			// What is between the tags is markdown of its own, which is what a person
			// writes without thinking about it.
			name:     "markdown inside a tint",
			markdown: "{{accent}}bold **inside** and `code`{{/}}",
			want:     `<span class="tint tint-accent">bold <strong>inside</strong> and <code>code</code></span>`,
		},
		{
			name:     "a tint inside emphasis",
			markdown: "**{{bad}}gone{{/}}**",
			want:     `<strong><span class="tint tint-bad">gone</span></strong>`,
		},
		{
			// Prose on a slide wraps, so a tint that ends a line down is the ordinary
			// case rather than the exception.
			name:     "a tint that closes on the next line",
			markdown: "opens {{good}}here and\ncloses{{/}} there",
			want:     `<span class="tint tint-good">here and`,
		},
		{
			name:     "a name that is not a role is left as written",
			markdown: "The {{acent}}fast{{/}} path",
			want:     "The {{acent}}fast{{/}} path",
			problems: 1,
		},
		{
			name:     "a role opened and never closed is left as written",
			markdown: "The {{good}}fast path",
			want:     "The {{good}}fast path",
			problems: 1,
		},
		{
			name:     "a close with nothing open is left as written",
			markdown: "The {{/}} path",
			want:     "The {{/}} path",
		},
		{
			name:     "a tag written with spaces is not a tint",
			markdown: "jet writes {{ raw: body }} in a template",
			want:     "{{ raw: body }}",
		},
		{
			name:     "a backslash keeps the braces",
			markdown: `\{{good}}not a tint`,
			want:     "{{good}}not a tint",
		},
		{
			name:     "code is not read for tints",
			markdown: "`{{good}}x{{/}}`",
			want:     "<code>{{good}}x{{/}}</code>",
		},
		{
			name:     "text inside a tint is escaped the way text is",
			markdown: `{{bad}}a < b & "c"{{/}}`,
			want:     `<span class="tint tint-bad">a &lt; b &amp; &quot;c&quot;</span>`,
		},
		{
			name:     "a tint in a table cell",
			markdown: "| a | b |\n|---|---|\n| {{good}}yes{{/}} | no |",
			want:     `<td><span class="tint tint-good">yes</span></td>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			md := newSlideMarkdown("github")
			md.resetTints()

			body, err := md.render(tc.markdown, false)
			if err != nil {
				t.Fatalf("cannot render: %v", err)
			}

			if !strings.Contains(body.Body, tc.want) {
				t.Errorf("rendered\n%s\nwant it to hold\n%s", body.Body, tc.want)
			}

			if len(md.tintProblems()) != tc.problems {
				t.Errorf("reported %v, want %d problems", md.tintProblems(), tc.problems)
			}
		})
	}
}

// TestBoldInsideATintTakesTheTintColour pins the rule that colours the bold, the
// link and the inline code inside a tint. A theme colours each of those itself,
// and a theme's rule beats an inherited colour, so without it
// {{accent}}**word**{{/}} came out bold in the colour of the body.
func TestBoldInsideATintTakesTheTintColour(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Number:    1,
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "# Setup\n\nThe {{accent}}**fast**{{/}} path\n",
	})

	for _, role := range tintRoles {
		if !strings.Contains(out.HTML, ".reveal .tint-"+role+" *") {
			t.Errorf("the page carries no rule for what a %s tint holds", role)
		}
	}

	if !strings.Contains(out.HTML, `<span class="tint tint-accent"><strong>fast</strong></span>`) {
		t.Error("the bold inside the tint is not inside the span")
	}
}

// TestTintProblemNamesTheRoles pins that the message a mistyped role produces
// says what the roles are, since the person reading it is looking at a word that
// came out the colour of the prose around it.
func TestTintProblemNamesTheRoles(t *testing.T) {
	md := newSlideMarkdown("github")
	md.resetTints()

	_, err := md.render("The {{acent}}fast{{/}} path", false)
	if err != nil {
		t.Fatalf("cannot render: %v", err)
	}

	problems := md.tintProblems()
	if len(problems) != 1 {
		t.Fatalf("reported %v, want one problem", problems)
	}

	for _, role := range tintRoles {
		if !strings.Contains(problems[0], role) {
			t.Errorf("the problem was %q, want it to name %q", problems[0], role)
		}
	}
}

// TestTintReachesEveryPlaceASlideWritesMarkdown pins the caption, the call to
// action and the notes, which are rendered by their own calls and would
// otherwise carry the braces onto the slide.
func TestTintReachesEveryPlaceASlideWritesMarkdown(t *testing.T) {
	md := newSlideMarkdown("github")

	inline, err := md.renderInline("a {{good}}caption{{/}}")
	if err != nil {
		t.Fatalf("cannot render the caption: %v", err)
	}

	if !strings.Contains(inline, `<span class="tint tint-good">caption</span>`) {
		t.Errorf("the caption was %q", inline)
	}

	block, err := md.renderMarkdown("a {{bad}}note{{/}}")
	if err != nil {
		t.Fatalf("cannot render the notes: %v", err)
	}

	if !strings.Contains(block, `<span class="tint tint-bad">note</span>`) {
		t.Errorf("the notes were %q", block)
	}
}

// TestTintProblemsAreReportedAgainstTheSlide pins that a mistyped role reaches
// the reader the way an unknown transition does, and that the slide still
// renders: one word the colour of the prose around it is not a slide to drop.
func TestTintProblemsAreReportedAgainstTheSlide(t *testing.T) {
	out := renderSlides(t, loadDefaultTheme(t), renderShow(), &Slide{
		Number:    1,
		Path:      "01-content.md",
		PageStyle: "content",
		Markdown:  "# Setup\n\nThe {{acent}}fast{{/}} path\n",
	})

	if len(out.Problems) != 1 {
		t.Fatalf("problems were %v, want one", out.Problems)
	}

	if out.Problems[0].Path != "01-content.md" {
		t.Errorf("the problem was against %q", out.Problems[0].Path)
	}

	if !strings.Contains(out.HTML, "The {{acent}}fast{{/}} path") {
		t.Error("the slide was dropped, want it rendered with the braces as written")
	}
}
