// Package ticketset is the aggregate of a set of exam tickets (TASK-133, «Билеты»): the list the student gave (pasted
// text or a library material), parsed strictly and deterministically on the desktop (ParseList), the student's
// confirmation of the paid run with its cost estimate, one answer with citations per ticket (coursework/answer) and
// its coverage, and the .docx filed into the subject's «Билеты».
//
// Statuses as a table: proposed («Найдено N билетов — подтвердить») → confirmed (the student's move only) → answering
// (one ticket per job step) → done (the file is filed) | failed | cancelled. The list text is untrusted data
// (ADR-0022): a ticket title is only ever a question to the reader and a heading of the file, never an instruction.
// A set keeps the answer ID and the coverage of each ticket, nothing of the lessons: the quotes and recordings stay in
// the answer aggregate.
package ticketset
