# Choria

The Choria logomark's blue as the ground, its green as the accent, white type,
Dosis throughout, and the mark itself painted large and faint into the bottom
right of every slide.

    theme: ../themes/choria

## Example

There is an example site at [https://presentmd-theme-choria.shipstatic.com](https://presentmd-theme-choria.shipstatic.com).

## What it is made of

| | |
|---|---|
| ground | `#2a395b`, the blue the square logomark sits on |
| accent | `#66c187`, the green of the mark |
| type | white, with the body at 90% and captions and the footer at 58% |
| headings, prose | Dosis, shipped as latin woff2 at 400, 500, 600 and 700 |
| code | the machine's mono, highlighted with chroma's `nord` |

All seven page styles are implemented, and `columns` splits on `---`.

## The watermark

`.slide-frame::before` and `.slide-title::before` draw `images/logomark.svg`
anchored past the top right corner, so it runs down and to the left across about
a third of the page. Two variables on `:root` move it:

```css
--mark-size: 620px;
--mark-opacity: 0.13;
```

Both selectors are scoped under `.slides`. Reveal copies a section's class onto
the element it draws that slide's background with, so a bare `.slide-title`
matches that element too and paints a second mark at the size of the window
rather than of the slide.

A slide that names a `background` of its own carries enough picture already, so
the mark is left off there.

## Backgrounds a slide sets itself

The type is white, so a slide naming `background:` wants a dark value. A light
one leaves white on light.

## Contacts

The closing slide reads the contacts from the deck's own frontmatter, not from
the theme:

```yaml
presenter:
  name: R.I.
  surname: Pienaar
contact_email: rip@devco.net
contact_web: https://devco.net
contact_web_label: blog
contact_social: "@ripienaar@mastodon.social"
contact_social_network: Mastodon
```

## Fonts

Dosis is under the SIL Open Font License. The subsets in `fonts/` were cut from
the Choria brand package with `pyftsubset --flavor=woff2` over the latin range.
