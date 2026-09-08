---
page_style: content
text_size: small
caption: A path rather than a name, resolved against the deck directory
cta: presentmd serve mydeck
notes: |
  A theme value holding a path separator is read as a directory. It is watched
  along with the deck, so editing theme.css reloads the browser while you write
  it.

  An export inlines the stylesheet and its fonts, so a deck rendered against a
  theme on your laptop opens anywhere.

  Start from present/themes/default/ in the repository rather than an empty
  directory.
---

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
