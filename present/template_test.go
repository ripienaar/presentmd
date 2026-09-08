package present

import (
	"strings"
	"testing"
)

// TestNewTemplateData pins what a template gets that no slide wrote: the
// presenter line, the avatar, the footer and the contacts, each with the empty
// case a presentation.yaml that sets nothing produces.
func TestNewTemplateData(t *testing.T) {
	cases := []struct {
		name      string
		show      Presentation
		presenter string
		avatar    string
		footer    []string
		contacts  []Contact
	}{
		{
			name: "footer set wins over the contacts",
			show: Presentation{
				Footer:               "planboard",
				Presenter:            Presenter{Name: "R.I.", Surname: "Pienaar", Avatar: "images/avatar.png", GitHub: "ripienaar"},
				ContactEmail:         "rip@devco.net",
				ContactWeb:           "https://devco.net",
				ContactSocial:        "@ripienaar",
				ContactSocialNetwork: "Mastodon",
			},
			presenter: "R.I. Pienaar",
			avatar:    "images/avatar.png",
			footer:    []string{"planboard"},
			contacts: []Contact{
				{Label: "Mastodon", Value: "@ripienaar"},
				{Label: "email", Value: "rip@devco.net"},
				{Label: "blog", Value: "https://devco.net"},
				{Label: "github", Value: "ripienaar"},
			},
		},
		{
			// The network is unset here, so the handle is labeled social rather than
			// with a network nobody named.
			name: "no footer builds one from the presenter and the contacts",
			show: Presentation{
				Presenter:     Presenter{Name: "R.I.", Surname: "Pienaar"},
				ContactEmail:  "rip@devco.net",
				ContactWeb:    "https://devco.net",
				ContactSocial: "@ripienaar",
			},
			presenter: "R.I. Pienaar",
			footer:    []string{"R.I. Pienaar", "rip@devco.net", "https://devco.net", "@ripienaar"},
			contacts: []Contact{
				{Label: "social", Value: "@ripienaar"},
				{Label: "email", Value: "rip@devco.net"},
				{Label: "blog", Value: "https://devco.net"},
			},
		},
		{
			name:      "the github handle stands in for the avatar",
			show:      Presentation{Presenter: Presenter{Name: "R.I.", GitHub: "ripienaar"}},
			presenter: "R.I.",
			avatar:    "https://github.com/ripienaar.png",
			footer:    []string{"R.I."},
			contacts:  []Contact{{Label: "github", Value: "ripienaar"}},
		},
		{
			name:      "a surname alone carries no stray space",
			show:      Presentation{Presenter: Presenter{Surname: "Pienaar"}},
			presenter: "Pienaar",
			footer:    []string{"Pienaar"},
		},
		{
			name: "a presentation that sets none of it",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			show := tc.show

			data := NewTemplateData(&show)

			if data.Presentation != &show {
				t.Error("the presentation the template reaches is not the one given")
			}

			if data.Presenter != tc.presenter {
				t.Errorf("presenter %q, want %q", data.Presenter, tc.presenter)
			}

			if data.Avatar != tc.avatar {
				t.Errorf("avatar %q, want %q", data.Avatar, tc.avatar)
			}

			if strings.Join(data.Footer, "|") != strings.Join(tc.footer, "|") {
				t.Errorf("footer %v, want %v", data.Footer, tc.footer)
			}

			if len(data.Contacts) != len(tc.contacts) {
				t.Fatalf("contacts %v, want %v", data.Contacts, tc.contacts)
			}

			for i, want := range tc.contacts {
				if data.Contacts[i] != want {
					t.Errorf("contact %d is %v, want %v", i, data.Contacts[i], want)
				}
			}
		})
	}
}

// TestTemplateDataVars pins the variable names themselves, since they are what a
// theme's templates say and renaming one breaks every theme the binary cannot
// reach.
func TestTemplateDataVars(t *testing.T) {
	data := NewTemplateData(&Presentation{Presenter: Presenter{Name: "R.I."}})
	data.Slide = &Slide{PageStyle: "content"}

	vars := data.vars()

	want := []string{
		"presentation",
		"slide",
		"heading",
		"body",
		"left",
		"right",
		"caption",
		"cta",
		"presenter",
		"avatar",
		"footer",
		"contacts",
	}

	for _, name := range want {
		_, ok := vars[name]
		if !ok {
			t.Errorf("a template cannot reach %q", name)
		}
	}

	if len(vars) != len(want) {
		t.Errorf("%d variables, want the %d a template is written against", len(vars), len(want))
	}
}

// TestSocialLabelIsTheNetwork covers the label the closing slide shows a handle
// under. A handle says nothing about which network it belongs to, so a deck that
// names no network gets a label that claims none.
func TestSocialLabelIsTheNetwork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		network string
		want    string
	}{
		{name: "the network the deck named", network: "Bluesky", want: "Bluesky"},
		{name: "no network", network: "", want: "social"},
		{name: "spaces alone are no network", network: "   ", want: "social"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			show := &Presentation{ContactSocial: "@me@example.social", ContactSocialNetwork: tc.network}

			contacts := contactsFor(show)
			if len(contacts) != 1 {
				t.Fatalf("contacts were %v, want the handle alone", contacts)
			}

			if contacts[0].Label != tc.want {
				t.Errorf("label was %q, want %q", contacts[0].Label, tc.want)
			}
		})
	}
}

// TestWebLabelIsTheLabelTheDeckNamed pins the label on the web address, and
// that a deck naming none reads blog.
func TestWebLabelIsTheLabelTheDeckNamed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		label string
		want  string
	}{
		{name: "the label the deck named", label: "homepage", want: "homepage"},
		{name: "no label", label: "", want: "blog"},
		{name: "spaces alone are no label", label: "   ", want: "blog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			show := &Presentation{ContactWeb: "https://devco.net", ContactWebLabel: tc.label}

			contacts := contactsFor(show)
			if len(contacts) != 1 {
				t.Fatalf("contacts were %v, want the address alone", contacts)
			}

			if contacts[0].Label != tc.want {
				t.Errorf("label was %q, want %q", contacts[0].Label, tc.want)
			}
		})
	}
}

// TestSocialNetworkWithoutAHandleShowsNothing pins that naming a network and no
// handle puts no empty row on the closing slide.
func TestSocialNetworkWithoutAHandleShowsNothing(t *testing.T) {
	contacts := contactsFor(&Presentation{ContactSocialNetwork: "Bluesky"})
	if len(contacts) != 0 {
		t.Errorf("contacts were %v, want none", contacts)
	}
}
