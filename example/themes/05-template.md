---
page_style: code
text_size: small
caption: styles/content.jet, the template rendering this deck's ordinary slides
notes: |
  Jet escapes every expression by default. The values already rendered from
  markdown are written with raw:, and everything from the frontmatter is written
  plain so a title holding an ampersand cannot reach the page as markup.

  Getting that backwards is how a theme introduces a hole the renderer cannot
  close for it.
---

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
