package values

// Document is a homework document as the student saves or sends it (TASK-094): an optional title page, then blocks of
// text. It is a format-neutral model: an adapter renders it (Word .docx now; other formats may follow). Texts are plain
// text, never markup: the renderer escapes them for its format.
type Document struct {
	Title  TitlePage // the zero TitlePage: no title page
	Blocks []Block
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

// Block kinds.
const (
	BlockHeading   BlockKind = "heading"
	BlockParagraph BlockKind = "paragraph"
)

// Heading levels of a document: 1 is the top one.
const (
	MinHeadingLevel = 1
	MaxHeadingLevel = 3
)

// Block is one heading or paragraph. A heading has one line and a Level from MinHeadingLevel to MaxHeadingLevel; a
// paragraph has one or more lines, which the renderer breaks as they are (a list typed line by line stays a list).
type Block struct {
	Kind  BlockKind
	Level int
	Lines []string
}
