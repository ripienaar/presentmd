---
page_style: code
text_size: small
caption: theme.yaml, every key a theme may set
notes: |
  code_style is a chroma style name. split_styles lists the page styles a
  thematic break divides into two columns; a style left out of it treats --- as
  a horizontal rule. transition is the theme's own move, and a deck that names
  one overrides it.

  The three font stacks reach the stylesheet as custom properties, so the CSS
  reads var(--font-body) rather than naming a family twice.
---

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
