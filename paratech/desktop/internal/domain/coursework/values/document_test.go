package values

import (
	"errors"
	"strings"
	"testing"
)

func lines(n int, s string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func paragraphs(n int) []Block {
	out := make([]Block, n)
	for i := range out {
		out[i] = Block{Kind: BlockParagraph, Lines: []string{"a"}}
	}
	return out
}

// lists returns n bullet lists of items empty items each.
func lists(n, items int) []Block {
	out := make([]Block, n)
	for i := range out {
		out[i] = Block{Kind: BlockBulletList, Lines: make([]string, items)}
	}
	return out
}

// tables returns n tables of rows rows (the header included): a header of MaxTableColumns empty cells, then rows
// with one empty cell each, padded to MaxTableColumns when rendered.
func tables(n, rows int) []Block {
	out := make([]Block, n)
	for i := range out {
		r := make([][]string, rows-1)
		for j := range r {
			r[j] = []string{""}
		}
		out[i] = Block{Kind: BlockTable, Table: Table{Header: make([]string, MaxTableColumns), Rows: r}}
	}
	return out
}

func TestDocumentValidate(t *testing.T) {
	long := strings.Repeat("я", MaxTextRunes)
	cases := []struct {
		name string
		doc  Document
		ok   bool
	}{
		{"empty", Document{}, true},
		{"every kind", Document{Blocks: []Block{
			{Kind: BlockHeading, Level: 1, Lines: []string{"Введение"}},
			{Kind: BlockParagraph, Lines: []string{"текст"}},
			{Kind: BlockBulletList, Lines: []string{"а", "б"}},
			{Kind: BlockNumberedList, Lines: []string{"а", "б"}},
			{Kind: BlockTable, Table: Table{Rows: [][]string{{"1"}}}, Caption: "Данные"},
			{Kind: BlockCaption, Lines: []string{"Рисунок 1 – Схема"}},
			{Kind: BlockBibliography, Lines: []string{"Иванов И. И. Книга. – М., 2020."}},
			{Kind: BlockPlaceholder, Lines: []string{"Вставьте график"}, Caption: "График"},
		}}, true},
		{"unknown kind", Document{Blocks: []Block{{Kind: "script"}}}, false},
		{"zero kind", Document{Blocks: []Block{{Lines: []string{"x"}}}}, false},
		{"blocks at the limit", Document{Blocks: paragraphs(MaxDocumentBlocks)}, true},
		{"too many blocks", Document{Blocks: paragraphs(MaxDocumentBlocks + 1)}, false},
		{"lines at the limit", Document{Blocks: []Block{{Kind: BlockBulletList, Lines: lines(MaxBlockLines, "a")}}}, true},
		{"too many lines", Document{Blocks: []Block{{Kind: BlockBulletList, Lines: lines(MaxBlockLines+1, "a")}}}, false},
		{"text at the limit", Document{Blocks: []Block{{Kind: BlockParagraph, Lines: []string{long}}}}, true},
		{"text too long", Document{Blocks: []Block{{Kind: BlockParagraph, Lines: []string{long + "я"}}}}, false},
		{"caption too long", Document{Blocks: []Block{{Kind: BlockPlaceholder, Caption: long + "я"}}}, false},
		{"title too long", Document{Title: TitlePage{Topic: long + "я"}}, false},
		{"document too long", Document{Blocks: []Block{
			{Kind: BlockParagraph, Lines: lines(MaxDocumentRunes/MaxTextRunes+1, long)},
		}}, false},
		{"empty table", Document{Blocks: []Block{{Kind: BlockTable}}}, false},
		{"table of empty rows", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{Rows: [][]string{{}, {}}}}}}, false},
		{"header only", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{Header: []string{"a"}}}}}, true},
		{"rows at the limit", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{
			Header: []string{"a"}, Rows: make([][]string, MaxTableRows-1),
		}}}}, true},
		{"too many rows", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{
			Header: []string{"a"}, Rows: make([][]string, MaxTableRows),
		}}}}, false},
		{"columns at the limit", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{Rows: [][]string{lines(MaxTableColumns, "")}}}}}, true},
		{"too many columns", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{Rows: [][]string{{"a"}, lines(MaxTableColumns+1, "")}}}}}, false},
		{"cell too long", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{Rows: [][]string{{long + "я"}}}}}}, false},
		{"header cell too long", Document{Blocks: []Block{{Kind: BlockTable, Table: Table{Header: []string{long + "я"}}}}}, false},
		// Structure limits: empty items and cells cost no characters but still render.
		{"items at the document limit", Document{Blocks: lists(MaxDocumentItems/MaxBlockLines, MaxBlockLines)}, true},
		{"too many items in the document", Document{Blocks: append(lists(MaxDocumentItems/MaxBlockLines, MaxBlockLines),
			Block{Kind: BlockParagraph, Lines: []string{""}})}, false},
		{"paragraph lines count as items", Document{Blocks: append(lists(MaxDocumentItems/MaxBlockLines, MaxBlockLines-1),
			Block{Kind: BlockParagraph, Lines: lines(MaxDocumentItems/MaxBlockLines+1, "")})}, false},
		{"cells at the document limit", Document{Blocks: tables(MaxDocumentCells/(MaxTableRows*MaxTableColumns), MaxTableRows)}, true},
		{"too many cells in the document", Document{Blocks: append(tables(MaxDocumentCells/(MaxTableRows*MaxTableColumns), MaxTableRows),
			Block{Kind: BlockTable, Table: Table{Rows: [][]string{{"a"}}}})}, false},
		{"short rows count as padded", Document{Blocks: tables(MaxDocumentCells/(MaxTableRows/2*MaxTableColumns)+1, MaxTableRows/2)}, false},
		{"a table of another kind is not checked", Document{Blocks: []Block{{Kind: BlockParagraph, Table: Table{Rows: make([][]string, MaxTableRows+1)}}}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.doc.Validate()
			if tc.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalidDocument) {
				t.Fatalf("err = %v, want ErrInvalidDocument", err)
			}
		})
	}
}

// Validate must not write into the caller's slices (a block is a value but its slices are shared).
func TestDocumentValidateDoesNotWrite(t *testing.T) {
	backing := []string{"a", "b", "keep"}
	doc := Document{Blocks: []Block{{Kind: BlockParagraph, Lines: backing[:2], Caption: "x"}}}
	if err := doc.Validate(); err != nil {
		t.Fatal(err)
	}
	if backing[2] != "keep" {
		t.Fatalf("backing = %q", backing)
	}
}

func TestTableColumns(t *testing.T) {
	if n := (Table{Header: []string{"a", "b"}, Rows: [][]string{{"1"}, {"1", "2", "3"}}}).Columns(); n != 3 {
		t.Fatalf("columns = %d", n)
	}
	if n := (Table{}).Columns(); n != 0 {
		t.Fatalf("columns = %d", n)
	}
}

func TestDocumentWording(t *testing.T) {
	cases := []struct{ got, want string }{
		{TableCaption(2, "Результаты"), "Таблица 2 – Результаты"},
		{TableCaption(1, ""), "Таблица 1"},
		{FigureCaption(3, "Схема"), "Рисунок 3 – Схема"},
		{FigureCaption(1, ""), "Рисунок 1"},
		{PlaceholderText("Вставьте график"), "▢ Вставьте график"},
		{PlaceholderText(""), "▢"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}
