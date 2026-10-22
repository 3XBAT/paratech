// Package doctext reads the text of a library file (TASK-122, ADR-0024): DOCX paragraphs in order, PPTX slides in
// presentation order, TXT in UTF-8 or UTF-16 with a byte order mark. Only the standard library is used
// (archive/zip, encoding/xml). PDF text is not read (material.ErrTextNotExtracted).
//
// The files are untrusted: the number of ZIP entries, the unpacked size of every part read and of all of them, and
// the text kept are bounded, so a ZIP bomb or a huge XML part ends as material.ErrTextUnreadable instead of filling
// the memory. Entry names are only looked up as keys, never used as paths; a slide target outside ppt/ is refused.
// encoding/xml expands no entity beyond the predefined ones, so there is no entity bomb.
//
// Locators: PPTX gives slide numbers. DOCX gives page numbers when the file has page breaks (explicit ones or those
// Word recorded at its last layout); TXT when it has form feeds. Otherwise the text is cut into fragments of bounded
// size (material.FragmentsOf).
package doctext
