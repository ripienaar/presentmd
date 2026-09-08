---
page_style: content
caption: One template per page style, plus the partials they include
cta: A slide names a page style, the theme decides its look
notes: |
  A page style is the contract between a deck and a theme. A slide names one and
  the theme renders that name, so the same deck through another theme is another
  look with no slide edited.

  A partial is any file the templates include. The underscore is a convention,
  not a rule.
---

# Three Kinds of File

- `theme.yaml` sets the code style, the fonts and the splitting page styles
- `theme.css` is the whole look, and names its own font files
- `styles/<name>.jet` renders one page style, one file each

A deck reaches a theme of its own with a path: `theme: ./mytheme`.
