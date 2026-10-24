// Package tickets is the use case of «Билеты» (TASK-133): a list of exam tickets (pasted, or the text of a library
// material) is parsed strictly on the desktop into a proposed set («Найдено N билетов — подтвердить»); only the
// student's Confirm (input/wails) starts the paid run, a ticket_answers job that answers one ticket per step with
// citations from the subject's lessons (an Answerer port, wired to cases/answering by the app), then files the .docx
// into the subject's «Билеты». The agent has no move here (no tool yet: it needs a Hub catalog version).
package tickets
