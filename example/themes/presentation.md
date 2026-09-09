---
title: Writing a Theme
subtitle: What a deck hands a theme, and what a theme does with it
event: Announcing presentmd
date: "2026-09-08"
theme: default
aspect: "16:9"
transition: fade
presenter:
  name: R.I.
  surname: Pienaar
  avatar: https://www.gravatar.com/avatar/9482a1c5a9c64c5d7296971f030165b7?s=1024
contact_email: rip@devco.net
contact_web: https://devco.net
contact_social: "@ripienaar@devco.social"
contact_social_network: Mastodon
---

+++
page_style: title
notes: |-
  This deck is rendered by the theme it describes. Every slide you see is one of
  the templates under discussion.
  
  Press S for the speaker view, ESC for the overview.
+++

+++
page_style: section
notes: A theme is three kinds of file in one directory.
+++

# A Theme Is a Directory

+++
page_style: content
caption: One template per page style, plus the partials they include
cta: A slide names a page style, the theme decides its look
notes: |-
  A page style is the contract between a deck and a theme. A slide names one and
  the theme renders that name, so the same deck through another theme is another
  look with no slide edited.
  
  A partial is any file the templates include. The underscore is a convention,
  not a rule.
+++

# Three Kinds of File

- `theme.yaml` sets the code style, the fonts and the splitting page styles
- `theme.css` is the whole look, and names its own font files
- `styles/<name>.jet` renders one page style, one file each

A deck reaches a theme of its own with a path: `theme: ./mytheme`

+++
page_style: code
caption: theme.yaml, every key a theme may set
notes: |-
  code_style is a chroma style name. split_styles lists the page styles a
  thematic break divides into two columns; a style left out of it treats --- as
  a horizontal rule. transition is the theme's own move, and a deck that names
  one overrides it.
  
  The three font stacks reach the stylesheet as custom properties, so the CSS
  reads var(--font-body) rather than naming a family twice.
text_size: small
+++

# theme.yaml

```yaml
code_style: github

split_styles:
  - columns

transition: none

fonts:
  heading: '"IBM Plex Sans Condensed", ui-sans-serif, sans-serif'
  body: '"IBM Plex Sans", ui-sans-serif, system-ui, sans-serif'
  mono: '"IBM Plex Mono", ui-monospace, Menlo, monospace'
```

+++
page_style: code
caption: styles/content.jet, the template rendering this deck's ordinary slides
notes: |-
  Jet escapes every expression by default. The values already rendered from
  markdown are written with raw:, and everything from the frontmatter is written
  plain so a title holding an ampersand cannot reach the page as markup.
  
  Getting that backwards is how a theme introduces a hole the renderer cannot
  close for it.
text_size: small
+++

# A Page Style

```html
<div class="slide-frame">
  {{ if heading != "" }}
    <h1 class="slide-heading">{{ raw: heading }}</h1>
  {{ end }}
  <div class="slide-body">
    <div class="body">{{ raw: body }}</div>
  </div>
  {{ include "_footer.jet" }}
</div>
```

+++
page_style: content
caption: Every variable a template is handed
notes: |-
  heading, body, left and right are already HTML, rendered from the slide's
  markdown before the template runs. left and right are filled only for a
  splitting page style, and body is empty on that slide.
  
  presenter, avatar, footer and contacts come from presentation.yaml, so no
  slide writes them and every theme places them the same way.
text_size: small
+++

# What a Template Gets

| Variable | What it holds |
|----------|---------------|
| `heading` `body` | the slide's heading and content, as HTML |
| `left` `right` | the two halves of a splitting slide |
| `caption` `cta` | the two lines under the body |
| `presenter` `avatar` | the name and the picture |
| `footer` `contacts` | the footer parts, and the labeled handles |
| `presentation` `slide` | the yaml and this slide, whole |

+++
page_style: columns
caption: This slide is the columns template, reading left and right
cta: split_styles is what fills them
notes: |-
  A splitting page style is handed left and right instead of body. The renderer
  divides the markdown at the thematic break and the template places the two
  halves; a theme that lists a style in split_styles and never reads left and
  right renders an empty slide.
+++

# Who Decides What

The deck

1. Which page style
2. The words
3. Where the break falls

---

The theme

1. What that style looks like
2. Type, color, spacing
3. Whether the style splits at all

+++
page_style: code
caption: theme.css, where the font stacks arrive and the ground is painted
notes: |-
  Paint the ground on .reveal-viewport alone. Reveal draws a slide's own
  background in a layer behind .slides, so painting .slides as well covers it
  and a slide's background shows as a border around the frame.
  
  Font files the stylesheet names with url() are served from the theme
  directory, and render inlines them as data URIs.
text_size: small
+++

# theme.css

```css
:root {
  --bg: #f3efe6;
  --ink: #1d2b2a;
  --accent: #0f6f6a;
}

/* the paper, and nothing over a slide's own background */
.reveal-viewport { background: var(--bg); }

.reveal .slide-body { font-family: var(--font-body); }
.reveal .slide-heading { font-family: var(--font-heading); }
```

+++
page_style: content
caption: A theme that spells no rule for a step renders that slide at the normal size
notes: |-
  A slide asks for a step by name and the theme decides what the step means, so
  a deck of slides at four sizes still looks like one deck.
  
  The renderer accepts all four whatever the theme spells, so a step with no
  rule renders normal and the author is told nothing. Spell all three.
text_size: small
+++

# Four Text Sizes

A slide sets `text_size` to `small`, `normal`, `large` or `huge`, and the theme
sizes the body and the code together.

```css
.reveal section[data-text-size="small"] { --body-scale: 0.85; }
.reveal section[data-text-size="large"] { --body-scale: 1.25; }
.reveal section[data-text-size="huge"]  { --body-scale: 1.6; }
```

+++
page_style: content
caption: A path rather than a name, resolved against the deck directory
cta: presentmd serve mydeck
notes: |-
  A theme value holding a path separator is read as a directory. It is watched
  along with the deck, so editing theme.css reloads the browser while you write
  it.
  
  An export inlines the stylesheet and its fonts, so a deck rendered against a
  theme on your laptop opens anywhere.
  
  Start from present/themes/default/ in the repository rather than an empty
  directory.
text_size: small
+++

# A Theme of Your Own

```
mydeck/
  presentation.yaml     theme: ./mytheme
  01-title.md
  mytheme/
    theme.yaml
    theme.css
    styles/title.jet
    styles/content.jet
```

+++
page_style: closing
cta: github.com/ripienaar/presentmd
notes: The closing template reads contacts, which the deck never writes into a slide.
+++

# Questions?
