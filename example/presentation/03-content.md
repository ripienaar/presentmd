---
page_style: content
caption: One file per slide, ordered by the number in front of its name
cta: presentmd serve example/presentation
notes: |
  The first level one heading in the body becomes the slide heading and the rest
  is the content. The theme places the caption and the call to action.
---

# A Directory of Files

A deck is a directory holding `presentation.yaml` and one markdown file per
slide. The **number in front of the filename** sets the order, so `01-title.md`
comes before `02-section.md`.

- `presentation.yaml` sets the title, the theme and the presenter
- each `NN-name.md` is one slide
- `images/` holds whatever the slides point at

Read the [reference](https://github.com/ripienaar/presentmd) for every key.
