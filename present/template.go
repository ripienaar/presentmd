package present

import (
	"strings"

	"github.com/CloudyKit/jet/v6"
)

// Contact is one contact and the label a theme shows it under. The pairs come
// from presentation.yaml, so a closing slide carries them without any slide
// naming them.
type Contact struct {
	Label string
	Value string
}

// TemplateData is what a page style template is executed with. The Go field
// names matter less than the variable names vars gives them: those are the
// contract every theme's templates are written against, including a directory
// theme this repository never sees, so renaming one breaks themes it cannot
// reach.
//
// Heading, Body, Left, Right, Caption and CTA are already HTML, rendered from
// markdown before the template runs. Jet escapes by default and a template opts
// out per expression, so those are written {{ raw: body }} and everything from
// the frontmatter is written plain. That is a rule the templates follow rather
// than one the renderer can enforce.
type TemplateData struct {
	Presentation *Presentation
	Slide        *Slide
	// Heading is the slide's lifted level-one heading, and is empty when the body
	// opened with none.
	Heading string
	Body    string
	// Left and Right are the halves of a body split at its thematic break. They
	// are empty for every style the theme does not name in split_styles.
	Left    string
	Right   string
	Caption string
	CTA     string
	// Presenter is the name and surname as one string.
	Presenter string
	// Avatar is the image path presentation.yaml gave, or the GitHub avatar URL
	// the handle stands in for, and is empty when there is neither.
	Avatar string
	// Footer is the footer line in parts, which the template joins with whatever
	// separator its design wants.
	Footer   []string
	Contacts []Contact
}

// NewTemplateData fills the parts that come from presentation.yaml alone: the
// presenter line, the avatar, the footer and the contacts. The caller adds the
// slide and its rendered HTML. Building them here rather than in the renderer is
// what keeps a served deck, an exported one and a test agreeing on what the
// footer says.
func NewTemplateData(show *Presentation) TemplateData {
	presenter := presenterName(show.Presenter)

	return TemplateData{
		Presentation: show,
		Presenter:    presenter,
		Avatar:       avatarFor(show.Presenter),
		Footer:       footerParts(show, presenter),
		Contacts:     contactsFor(show),
	}
}

// vars names the data for jet. A template reaches presentation, slide, heading,
// body, left, right, caption, cta, presenter, avatar, footer and contacts, and
// nothing else.
func (d TemplateData) vars() jet.VarMap {
	return jet.VarMap{}.
		Set("presentation", d.Presentation).
		Set("slide", d.Slide).
		Set("heading", d.Heading).
		Set("body", d.Body).
		Set("left", d.Left).
		Set("right", d.Right).
		Set("caption", d.Caption).
		Set("cta", d.CTA).
		Set("presenter", d.Presenter).
		Set("avatar", d.Avatar).
		Set("footer", d.Footer).
		Set("contacts", d.Contacts)
}

// presenterName joins the name and the surname, which are separate fields
// because a theme may place the surname on its own. A presenter with one of the
// two gets that one and no stray space around it.
func presenterName(presenter Presenter) string {
	return strings.Join(nonEmpty(presenter.Name, presenter.Surname), " ")
}

// avatarFor prefers the path presentation.yaml gave and falls back to the
// avatar the GitHub handle stands in for. The URL is built rather than fetched:
// under the board a deck loads on every request, and reaching the network is the
// browser's job when the deck is served and the exporter's when it is written.
func avatarFor(presenter Presenter) string {
	if presenter.Avatar != "" {
		return presenter.Avatar
	}

	if presenter.GitHub != "" {
		return "https://github.com/" + presenter.GitHub + ".png"
	}

	return ""
}

// footerParts is the footer presentation.yaml asked for, and otherwise the
// presenter and their contacts, which is what a deck that set no footer wants on
// every slide.
func footerParts(show *Presentation, presenter string) []string {
	if show.Footer != "" {
		return []string{show.Footer}
	}

	return nonEmpty(presenter, show.ContactEmail, show.ContactWeb, show.ContactSocial)
}

// contactsFor pairs each contact with the label a theme shows it under. The
// label for the web address is blog rather than web because that is what a
// person in the audience reads, and the social handle is labeled with the
// network the deck named it on.
func contactsFor(show *Presentation) []Contact {
	pairs := []Contact{
		{Label: socialLabel(show), Value: show.ContactSocial},
		{Label: "email", Value: show.ContactEmail},
		{Label: "blog", Value: show.ContactWeb},
		{Label: "github", Value: show.Presenter.GitHub},
	}

	var contacts []Contact

	for _, pair := range pairs {
		if pair.Value == "" {
			continue
		}

		contacts = append(contacts, pair)
	}

	return contacts
}

// socialLabel is the network the social handle is on, as the deck wrote it. A
// deck that gives a handle and no network gets social: the handle does not say
// which network it belongs to, and naming the wrong one is worse than naming
// none.
func socialLabel(show *Presentation) string {
	network := strings.TrimSpace(show.ContactSocialNetwork)
	if network == "" {
		return "social"
	}

	return network
}

// nonEmpty drops the values presentation.yaml did not set, so an absent field
// leaves no gap in a line built from several of them.
func nonEmpty(values ...string) []string {
	var out []string

	for _, value := range values {
		if value == "" {
			continue
		}

		out = append(out, value)
	}

	return out
}
