package values

import "strconv"

// Document is a student paper as the student saves or sends it (TASK-094, TASK-104): an optional title page, an
// optional table of contents, then blocks. It is a format-neutral model: an adapter renders it (Word .docx now; a
// .pptx or PDF renderer may reuse it) and owns every layout decision the model leaves open.
//
// Texts are plain text, never markup: the renderer escapes them for its format. A document built from model output is
// untrusted data (hard rule 3): check it with Validate before rendering; renderers refuse a document that fails it.
//
// Who builds it: «Сохранить в Word» (services/homeworkdoc: headings and paragraphs), finished papers (TASK-105: every
// block kind, a table of contents, a bibliography), lab reports (TASK-107: placeholders for the student's data).
type Document struct {
	Title           TitlePage // the zero TitlePage: no title page
	TableOfContents bool      // a table of contents after the title page, built from the headings
	Format          Formatting
	Blocks          []Block
}

// TitlePage are the lines of the title page, each already worded (domain/coursework/services/homeworkdoc); an empty
// line is left out, so nothing dangles («Проверил:» without a name).
type TitlePage struct {
	University string // top, centred
	Faculty    string
	Heading    string // «Домашнее задание по дисциплине «…»»
	Topic      string // the assignment title
	Performer  string // «Выполнил: студент группы … …»
	Checker    string // «Проверил: …»
	Place      string // bottom: city and year
}

// IsZero reports whether the title page has no line.
func (t TitlePage) IsZero() bool { return t == TitlePage{} }

// BlockKind is what a block of the document is.
type BlockKind string

// Block kinds. The fields of Block each kind reads are listed on Block.
const (
	BlockHeading      BlockKind = "heading"
	BlockParagraph    BlockKind = "paragraph"
	BlockBulletList   BlockKind = "bullets"
	BlockNumberedList BlockKind = "numbered" // numbered from 1, every list on its own
	BlockTable        BlockKind = "table"
	BlockCaption      BlockKind = "caption"      // a caption written as is, e.g. under a figure the student pastes
	BlockBibliography BlockKind = "bibliography" // the list of sources under BibliographyHeading
	BlockPlaceholder  BlockKind = "placeholder"  // a visible gap the student fills in (data, measurements, screenshots)
)

// Heading levels of a document: 1 is the top one.
const (
	MinHeadingLevel = 1
	MaxHeadingLevel = 3
)

// Block is one block of the document. By Kind:
//   - BlockHeading: Lines joined by spaces, Level from MinHeadingLevel to MaxHeadingLevel (a level out of range is
//     clamped). A level-1 heading starts a new page (GOST 7.32).
//   - BlockParagraph: Lines, which the renderer breaks as they are (a list typed line by line stays a list).
//   - BlockBulletList, BlockNumberedList: Lines are the items, one level.
//   - BlockTable: Table; Caption is the table name, numbered by TableCaption above the table.
//   - BlockCaption: Lines joined by spaces.
//   - BlockBibliography: Lines are the entries, numbered from 1, under BibliographyHeading.
//   - BlockPlaceholder: Lines joined by spaces are the prompt («Вставьте …»), shown by PlaceholderText; a non-empty
//     Caption names the figure the student inserts, numbered by FigureCaption below it.
type Block struct {
	Kind    BlockKind
	Level   int
	Lines   []string
	Table   Table
	Caption string
}

// Table is a simple table of plain-text cells. Header, when set, is the first row, repeated on every page. The column
// count is the longest row; shorter rows get empty cells.
type Table struct {
	Header []string
	Rows   [][]string
}

// Columns returns the column count of the table.
func (t Table) Columns() int {
	n := len(t.Header)
	for _, r := range t.Rows {
		n = max(n, len(r))
	}
	return n
}

// Fixed wording of a document (Russian, GOST 7.32).
const (
	ContentsHeading     = "СОДЕРЖАНИЕ"
	BibliographyHeading = "СПИСОК ИСПОЛЬЗОВАННЫХ ИСТОЧНИКОВ"
	PlaceholderMark     = "▢"
	captionDash         = " – "
	tableLabel          = "Таблица "
	figureLabel         = "Рисунок "
)

// TableCaption is the caption of the n-th table: «Таблица n – name», or «Таблица n» without a name.
func TableCaption(n int, name string) string { return numbered(tableLabel, n, name) }

// FigureCaption is the caption of the n-th figure: «Рисунок n – name», or «Рисунок n» without a name.
func FigureCaption(n int, name string) string { return numbered(figureLabel, n, name) }

func numbered(label string, n int, name string) string {
	s := label + strconv.Itoa(n)
	if name == "" {
		return s
	}
	return s + captionDash + name
}

// PlaceholderText is the visible text of a placeholder: the mark, then the prompt («▢ Вставьте график»).
func PlaceholderText(prompt string) string {
	if prompt == "" {
		return PlaceholderMark
	}
	return PlaceholderMark + " " + prompt
}
