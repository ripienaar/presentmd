---
page_style: content
caption: A theme that spells no rule for a step renders that slide at the normal size
notes: |
  A slide asks for a step by name and the theme decides what the step means, so
  a deck of slides at four sizes still looks like one deck.

  The renderer accepts all four whatever the theme spells, so a step with no
  rule renders normal and the author is told nothing. Spell all three.
---

# Four Text Sizes

A slide sets `text_size` to `small`, `normal`, `large` or `huge`, and the theme
sizes the body and the code together.

```css
.reveal section[data-text-size="small"] { --body-scale: 0.85; }
.reveal section[data-text-size="large"] { --body-scale: 1.25; }
.reveal section[data-text-size="huge"]  { --body-scale: 1.6; }
```
