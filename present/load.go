package present

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Errors returned by LoadDeck, which are the two failures that leave no deck to
// hold problems against. Everything else the loader meets is a Problem.
var (
	ErrOpenDeck       = errors.New("cannot open deck directory")
	ErrNoPresentation = errors.New("cannot read presentation.yaml")
)

const (
	presentationFile = "presentation.yaml"
	mdExt            = ".md"
	// defaultAspect is what a deck that names no aspect gets.
	defaultAspect = "16:9"
	fence         = "---"
	// byteOrderMark is stripped from the head of a slide so a file saved with one
	// still opens with its frontmatter fence.
	byteOrderMark = "\xef\xbb\xbf"
	// maxAspectTerm is the largest either side of a W:H ratio may be. Anything
	// past it is a typo rather than a slide shape, and multiplying it by the
	// width overflows the result into a negative height.
	maxAspectTerm = 10000
)

var (
	errNoFrontmatter           = errors.New("no yaml frontmatter")
	errUnterminatedFrontmatter = errors.New("unterminated yaml frontmatter")

	leadingDigitsRe = regexp.MustCompile(`^\d+`)
	aspectRe        = regexp.MustCompile(`^(\d+):(\d+)$`)
)

// Deck is one loaded deck directory: the talk, its slides in order, and the
// problems found reading them.
type Deck struct {
	// Dir is the deck directory as it was given to LoadDeck.
	Dir          string       `json:"dir"`
	Presentation Presentation `json:"presentation"`
	// Slides are the slides that loaded, sorted by number. One that could not be
	// read, and one with no page_style, is left out.
	Slides   []*Slide  `json:"slides"`
	Problems []Problem `json:"problems,omitempty"`

	root *os.Root
}

// Root is the open deck directory. Every asset a slide or a theme template
// names is read through it, so a path that leaves the deck is refused rather
// than served, which is the rule the board applies to its own root.
func (d *Deck) Root() *os.Root {
	return d.root
}

// Close releases the deck directory. A caller that reads assets holds the deck
// until it is done with them. A Deck that did not come from LoadDeck holds no
// root and closes without complaint.
func (d *Deck) Close() error {
	if d.root == nil {
		return nil
	}

	return d.root.Close()
}

// LoadDeck reads the deck in dir. It opens dir with os.OpenRoot and reads
// everything through that root, which reaches a deck that is itself a symlink
// and confines every later asset read to the directory.
//
// It returns an error only for what stops a deck existing: a directory it cannot
// open, and a missing presentation.yaml. A slide that will not parse, a missing
// page_style, two slides on one number and an unknown key in presentation.yaml
// are Problems on the returned deck, and what to do about one is the caller's:
// render refuses to write a file, serve logs it and serves the rest, and the
// board counts it.
func LoadDeck(dir string) (*Deck, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpenDeck, err)
	}

	rootFS := root.FS()

	data, err := fs.ReadFile(rootFS, presentationFile)
	if err != nil {
		root.Close()

		return nil, fmt.Errorf("%w: %w", ErrNoPresentation, err)
	}

	deck := &Deck{Dir: dir, Slides: []*Slide{}, root: root}

	deck.loadPresentation(data)
	deck.loadSlides(rootFS)

	return deck, nil
}

// loadPresentation decodes presentation.yaml. It decodes twice: once to fill the
// struct and once strictly, so a deck with a misspelled contact_socail still
// renders while the key that would otherwise vanish from every footer is
// reported.
func (d *Deck) loadPresentation(data []byte) {
	err := yaml.Unmarshal(data, &d.Presentation)
	if err != nil {
		d.report(presentationFile, fmt.Sprintf("cannot parse yaml: %s", oneLine(err)))

		// A deck with problems is still served, so a yaml the decoder rejected
		// carries on to the aspect below. Returning here left the width and height
		// at zero and reveal was initialized on a slide of no size.
		d.applyAspect()

		return
	}

	err = yaml.UnmarshalWithOptions(data, &Presentation{}, yaml.Strict())
	if err != nil {
		d.report(presentationFile, oneLine(err))
	}

	if d.Presentation.Theme == "" {
		d.report(presentationFile, "missing theme")
	}

	d.applyAspect()
}

// applyAspect fills the pixel pair the page hands reveal, falling back to the
// default for an aspect that is not a ratio.
func (d *Deck) applyAspect() {
	if d.Presentation.Aspect == "" {
		d.Presentation.Aspect = defaultAspect
	}

	width, height, ok := aspectSize(d.Presentation.Aspect)
	if !ok {
		d.report(presentationFile, fmt.Sprintf("aspect %q is not a W:H ratio", d.Presentation.Aspect))

		d.Presentation.Aspect = defaultAspect
		width, height, _ = aspectSize(defaultAspect)
	}

	d.Presentation.Width = width
	d.Presentation.Height = height
}

// aspectSize turns a W:H ratio into the pixel pair reveal is initialized with.
// The two ratios a deck actually names get the sizes their slides are drawn at,
// and anything else is scaled to the same width.
func aspectSize(aspect string) (int, int, bool) {
	switch aspect {
	case "16:9":
		return 1280, 720, true
	case "4:3":
		return 1024, 768, true
	}

	parts := aspectRe.FindStringSubmatch(aspect)
	if parts == nil {
		return 0, 0, false
	}

	width, err := strconv.Atoi(parts[1])
	if err != nil || width == 0 || width > maxAspectTerm {
		return 0, 0, false
	}

	height, err := strconv.Atoi(parts[2])
	if err != nil || height == 0 || height > maxAspectTerm {
		return 0, 0, false
	}

	return 1280, 1280 * height / width, true
}

// slideFront is a slide's frontmatter as written. Slide and Theme are pointers
// because their presence is what matters: a slide field makes a file a slide
// whatever its name, and a theme key is a problem whatever its value.
type slideFront struct {
	Slide      *int    `yaml:"slide,omitempty"`
	Theme      *string `yaml:"theme,omitempty"`
	PageStyle  string  `yaml:"page_style,omitempty"`
	Caption    string  `yaml:"caption,omitempty"`
	CTA        string  `yaml:"cta,omitempty"`
	Notes      string  `yaml:"notes,omitempty"`
	Background string  `yaml:"background,omitempty"`
	Transition string  `yaml:"transition,omitempty"`
	TextSize   string  `yaml:"text_size,omitempty"`
}

// loadSlides reads every markdown file in the deck directory. Files come back
// from ReadDir sorted by name, so the problems of a deck read in the order a
// person sees its files.
func (d *Deck) loadSlides(rootFS fs.FS) {
	entries, err := fs.ReadDir(rootFS, ".")
	if err != nil {
		d.report(".", fmt.Sprintf("cannot read deck directory: %s", err))

		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, mdExt) {
			continue
		}

		slide := d.loadSlide(rootFS, name)
		if slide == nil {
			continue
		}

		d.Slides = append(d.Slides, slide)
	}

	d.reportDuplicates()

	sort.SliceStable(d.Slides, func(i, j int) bool {
		if d.Slides[i].Number != d.Slides[j].Number {
			return d.Slides[i].Number < d.Slides[j].Number
		}

		return d.Slides[i].Path < d.Slides[j].Path
	})
}

// loadSlide reads one markdown file. It returns nil for a file that is not a
// slide, which is one whose name does not start with digits and whose
// frontmatter carries no slide field, so a README.md beside the slides costs
// nothing. It also returns nil for a slide that is a problem.
func (d *Deck) loadSlide(rootFS fs.FS, name string) *Slide {
	prefix := leadingDigitsRe.FindString(name)
	numbered := prefix != ""

	data, err := fs.ReadFile(rootFS, name)
	if err != nil {
		// A file that will not open cannot say whether it is a slide, so an
		// ordinary read failure is reported only when the name already claims to
		// be one. A path that leaves the deck is reported either way: it is the
		// refusal the deck root exists for, and staying quiet about it because the
		// file was named notes.md would hide it.
		if numbered || escapesDeck(err) {
			d.report(name, readMessage(err))
		}

		return nil
	}

	front, body, err := splitFrontmatter(data)
	if err != nil && !errors.Is(err, errNoFrontmatter) {
		// A file whose frontmatter will not open cannot say whether it is a slide,
		// so it is only reported when its name already claims to be one.
		if numbered {
			d.report(name, err.Error())
		}

		return nil
	}

	var parsed slideFront

	if front != "" {
		err = yaml.Unmarshal([]byte(front), &parsed)
		if err != nil {
			if numbered {
				d.report(name, fmt.Sprintf("cannot parse yaml frontmatter: %s", oneLine(err)))
			}

			return nil
		}
	}

	if !numbered && parsed.Slide == nil {
		return nil
	}

	if parsed.Theme != nil {
		d.report(name, "theme is set per presentation, not per slide")
	}

	if parsed.PageStyle == "" {
		d.report(name, "missing page_style")

		return nil
	}

	slide := &Slide{
		Path:       name,
		PageStyle:  parsed.PageStyle,
		Caption:    parsed.Caption,
		CTA:        parsed.CTA,
		Notes:      parsed.Notes,
		Background: parsed.Background,
		Transition: parsed.Transition,
		TextSize:   parsed.TextSize,
		Markdown:   strings.TrimSpace(body),
	}

	switch {
	case parsed.Slide != nil:
		slide.Number = *parsed.Slide

	case numbered:
		// The regexp matched digits, so the only way this fails is a prefix wider
		// than an int, which is not a slide number anybody meant.
		number, err := strconv.Atoi(prefix)
		if err != nil {
			d.report(name, fmt.Sprintf("filename number %q is not a slide number", prefix))

			return nil
		}

		slide.Number = number
	}

	return slide
}

// reportDuplicates names both files of every number two slides share. Neither is
// dropped: which of the two the author meant to renumber is not the loader's to
// guess.
func (d *Deck) reportDuplicates() {
	first := map[int]string{}

	for _, slide := range d.Slides {
		held, taken := first[slide.Number]
		if !taken {
			first[slide.Number] = slide.Path

			continue
		}

		d.report(slide.Path, fmt.Sprintf("slide number %d is already held by %s", slide.Number, held))
	}
}

func (d *Deck) report(path string, message string) {
	d.Problems = append(d.Problems, Problem{Path: path, Message: message})
}

// escapesDeck says whether a read was refused because the path left the deck
// directory. os.Root wraps an unexported sentinel for this, so the message is
// what there is to match on.
func escapesDeck(err error) bool {
	return strings.Contains(err.Error(), "path escapes from parent")
}

// readMessage phrases a file system error for the problems list, naming the
// confinement when a path escapes the deck directory.
func readMessage(err error) string {
	if escapesDeck(err) {
		return fmt.Sprintf("cannot read file: escapes the deck: %s", err)
	}

	return fmt.Sprintf("cannot read file: %s", err)
}

// splitFrontmatter divides a slide into its yaml frontmatter and the markdown
// body below it. It keys on the fence alone: the file has to open with one and
// the next line that is a fence on its own closes it. A file with no frontmatter
// comes back as body from the first byte, alongside errNoFrontmatter.
func splitFrontmatter(data []byte) (string, string, error) {
	text := strings.TrimPrefix(string(data), byteOrderMark)

	line, rest, found := strings.Cut(text, "\n")
	if !found || trimLine(line) != fence {
		return "", text, errNoFrontmatter
	}

	var front []string

	for {
		line, rest, found = strings.Cut(rest, "\n")

		if trimLine(line) == fence {
			return strings.Join(front, "\n"), rest, nil
		}

		if !found {
			return "", "", errUnterminatedFrontmatter
		}

		front = append(front, line)
	}
}

func trimLine(line string) string {
	return strings.TrimRight(line, " \t\r")
}

// oneLine takes the first line of a yaml decode error, which is where goccy
// writes the position and the message. The lines under it echo the offending
// source, which a problems list has no room for.
func oneLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")

	return strings.TrimSpace(line)
}
