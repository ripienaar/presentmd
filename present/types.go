// Package present loads a slide deck from a directory of markdown files and the
// presentation.yaml beside them. Nothing here renders markdown, reads a theme or
// touches the network: under the board a deck loads on every request, so a load
// is disk reads and a yaml decode and no more.
//
// A deck is written either way. One file per slide:
//
//	<deck>/
//	  presentation.yaml
//	  01-title.md
//	  02-what-it-is.md
//	  images/...
//
// or the whole deck in one, the presentation as its frontmatter and a +++ before
// each slide:
//
//	<deck>/
//	  presentation.md
//	  images/...
//
// A directory holding presentation.md is read that way, and the markdown beside
// it is not searched for slides.
//
// Slide bodies are kept as raw markdown. Lifting the heading out and splitting a
// columns slide both need the theme, which the loader does not have.
package present

// Problem is one file in a deck that could not be read, or that carries
// something the deck cannot use. present declares its own rather than reusing
// format.Problem: the server finds a project's decks after format has loaded the
// board and loads each through present, so present reporting format.Problem
// would put the two packages in a cycle the moment format grew a deck field. The
// server converts these when it builds the project JSON.
type Problem struct {
	// Path is the offending file relative to the deck directory.
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Presenter is who is giving the talk, placed by the theme template rather than
// written into any slide.
type Presenter struct {
	Name    string `json:"name,omitempty" yaml:"name,omitempty"`
	Surname string `json:"surname,omitempty" yaml:"surname,omitempty"`
	// Avatar is an image path relative to the deck directory.
	Avatar string `json:"avatar,omitempty" yaml:"avatar,omitempty"`
	// GitHub is the handle alone. The renderer turns it into
	// github.com/<handle>.png, since the loader never fetches.
	GitHub string `json:"github,omitempty" yaml:"github,omitempty"`
}

// Presentation is the deck's presentation.yaml: what the theme places around
// every slide, and the theme that places it.
type Presentation struct {
	Title    string `json:"title,omitempty" yaml:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty" yaml:"subtitle,omitempty"`
	Event    string `json:"event,omitempty" yaml:"event,omitempty"`
	Date     string `json:"date,omitempty" yaml:"date,omitempty"`
	// Theme names an embedded theme, or a directory when the value holds a path
	// separator. It is required: reveal applies one theme stylesheet to the whole
	// deck and there is no default to fall back to.
	Theme string `json:"theme,omitempty" yaml:"theme,omitempty"`
	// Aspect is a W:H ratio and defaults to 16:9.
	Aspect string `json:"aspect,omitempty" yaml:"aspect,omitempty"`
	// Transition is the move reveal makes between slides, one of reveal's own
	// none, fade, slide, convex, concave and zoom. It overrides the theme's, and
	// a deck and a theme that both name none get none: an animation is opted
	// into rather than out of.
	Transition string `json:"transition,omitempty" yaml:"transition,omitempty"`
	// TransitionSpeed is how fast that move runs, one of default, fast and slow.
	// A theme does not carry one: the speed belongs to the deck that asked for
	// the transition, and unset is reveal's own default.
	TransitionSpeed string    `json:"transition_speed,omitempty" yaml:"transition_speed,omitempty"`
	Footer          string    `json:"footer,omitempty" yaml:"footer,omitempty"`
	Presenter       Presenter `json:"presenter,omitzero" yaml:"presenter,omitempty"`
	ContactEmail    string    `json:"contact_email,omitempty" yaml:"contact_email,omitempty"`
	ContactWeb      string    `json:"contact_web,omitempty" yaml:"contact_web,omitempty"`
	// ContactWebLabel is the label the closing slide shows ContactWeb under, for
	// a deck whose address is a homepage or a docs site rather than a blog. A
	// deck that names none is labeled blog.
	ContactWebLabel string `json:"contact_web_label,omitempty" yaml:"contact_web_label,omitempty"`
	ContactSocial   string `json:"contact_social,omitempty" yaml:"contact_social,omitempty"`
	// ContactSocialNetwork is the network ContactSocial is a handle on, and is
	// the label the closing slide shows it under. A deck that names a handle and
	// no network is labeled social, since the handle alone does not say which
	// network it is on.
	ContactSocialNetwork string `json:"contact_social_network,omitempty" yaml:"contact_social_network,omitempty"`

	// Width and Height are Aspect in pixels, which is what the page hands reveal.
	// Every deck passes a pair because reveal's own default is 960 by 700 rather
	// than any of the ratios a deck names.
	Width  int `json:"width" yaml:"-"`
	Height int `json:"height" yaml:"-"`
}

// Slide is one markdown file of a deck: its frontmatter and its body as read.
type Slide struct {
	// Number is what the deck sorts on. It is the leading digits of the filename,
	// or the frontmatter's slide field where one is set, which is the override
	// for a slide whose name should not carry a number.
	Number int `json:"number"`
	// Path is the filename, which is the path relative to the deck directory.
	Path string `json:"path"`

	// PageStyle names the theme template this slide renders through. It is
	// required, and a slide without one does not enter the deck.
	PageStyle  string `json:"page_style,omitempty"`
	Caption    string `json:"caption,omitempty"`
	CTA        string `json:"cta,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Background string `json:"background,omitempty"`
	// Transition is reveal's per-slide override, for the one move in an
	// otherwise instant deck that wants to land. It takes the same values as the
	// presentation's.
	Transition string `json:"transition,omitempty"`
	// TextSize is the step the theme sets this slide's body and its code at, one
	// of small, normal, large and huge. It is the lever for the slide carrying
	// more than the rest and for the one carrying a sentence, and only the person
	// writing the slide knows which it is. A slide that names nothing is normal.
	TextSize string `json:"text_size,omitempty"`

	// Markdown is the body below the frontmatter, raw and unrendered.
	Markdown string `json:"markdown,omitempty"`
}
