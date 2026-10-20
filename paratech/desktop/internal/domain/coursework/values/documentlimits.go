package values

import (
	"fmt"
	"unicode/utf8"
)

// Limits of a document. A document built from model output may be anything, so a renderer must never be asked to
// build a huge or malformed file. They hold every «Сохранить в Word» document: a homework draft body
// (homeworkdraft.MaxBodyRunes) gives fewer blocks, lines and runes.
const (
	MaxDocumentBlocks = 10000     // blocks of a document
	MaxBlockLines     = 10000     // lines, list items or bibliography entries of a block
	MaxTextRunes      = 20000     // one line, cell or caption
	MaxDocumentRunes  = 1_000_000 // every text of a document together, title page included
	MaxTableRows      = 500       // header included
	MaxTableColumns   = 20
)

// Validate reports whether d is within the limits and has only known block kinds; a table needs at least one cell.
// The error wraps ErrInvalidDocument. The formatting profile is not checked: renderers use Formatting.Normalized.
func (d Document) Validate() error {
	if len(d.Blocks) > MaxDocumentBlocks {
		return fmt.Errorf("%w: %d blocks, at most %d", ErrInvalidDocument, len(d.Blocks), MaxDocumentBlocks)
	}
	total := 0
	count := func(s string) error {
		n := utf8.RuneCountInString(s)
		if n > MaxTextRunes {
			return fmt.Errorf("%w: a text of %d characters, at most %d", ErrInvalidDocument, n, MaxTextRunes)
		}
		total += n
		if total > MaxDocumentRunes {
			return fmt.Errorf("%w: more than %d characters", ErrInvalidDocument, MaxDocumentRunes)
		}
		return nil
	}
	t := d.Title
	for _, s := range []string{t.University, t.Faculty, t.Heading, t.Topic, t.Performer, t.Checker, t.Place} {
		if err := count(s); err != nil {
			return err
		}
	}
	for i, b := range d.Blocks {
		if err := b.validate(count); err != nil {
			return fmt.Errorf("block %d: %w", i, err)
		}
	}
	return nil
}

func (b Block) validate(count func(string) error) error {
	switch b.Kind {
	case BlockHeading, BlockParagraph, BlockBulletList, BlockNumberedList, BlockCaption, BlockBibliography,
		BlockPlaceholder, BlockTable:
	default:
		return fmt.Errorf("%w: unknown block kind %q", ErrInvalidDocument, b.Kind)
	}
	if len(b.Lines) > MaxBlockLines {
		return fmt.Errorf("%w: %d lines, at most %d", ErrInvalidDocument, len(b.Lines), MaxBlockLines)
	}
	for _, s := range b.Lines {
		if err := count(s); err != nil {
			return err
		}
	}
	if err := count(b.Caption); err != nil {
		return err
	}
	if b.Kind != BlockTable {
		return nil
	}
	return b.Table.validate(count)
}

func (t Table) validate(count func(string) error) error {
	rows := len(t.Rows)
	if len(t.Header) > 0 {
		rows++
	}
	cols := t.Columns()
	switch {
	case rows > MaxTableRows:
		return fmt.Errorf("%w: a table of %d rows, at most %d", ErrInvalidDocument, rows, MaxTableRows)
	case cols > MaxTableColumns:
		return fmt.Errorf("%w: a table of %d columns, at most %d", ErrInvalidDocument, cols, MaxTableColumns)
	case rows == 0 || cols == 0:
		return fmt.Errorf("%w: an empty table", ErrInvalidDocument)
	}
	for _, s := range t.Header {
		if err := count(s); err != nil {
			return err
		}
	}
	for _, r := range t.Rows {
		for _, s := range r {
			if err := count(s); err != nil {
				return err
			}
		}
	}
	return nil
}
