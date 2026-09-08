---
page_style: code
text_size: small
caption: theme.css, where the font stacks arrive and the ground is painted
notes: |
  Paint the ground on .reveal-viewport alone. Reveal draws a slide's own
  background in a layer behind .slides, so painting .slides as well covers it
  and a slide's background shows as a border around the frame.

  Font files the stylesheet names with url() are served from the theme
  directory, and render inlines them as data URIs.
---

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
