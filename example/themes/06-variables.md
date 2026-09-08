---
page_style: content
text_size: small
caption: Every variable a template is handed
notes: |
  heading, body, left and right are already HTML, rendered from the slide's
  markdown before the template runs. left and right are filled only for a
  splitting page style, and body is empty on that slide.

  presenter, avatar, footer and contacts come from presentation.yaml, so no
  slide writes them and every theme places them the same way.
---

# What a Template Gets

| Variable | What it holds |
|----------|---------------|
| `heading` `body` | the slide's heading and content, as HTML |
| `left` `right` | the two halves of a splitting slide |
| `caption` `cta` | the two lines under the body |
| `presenter` `avatar` | the name and the picture |
| `footer` `contacts` | the footer parts, and the labeled handles |
| `presentation` `slide` | the yaml and this slide, whole |
