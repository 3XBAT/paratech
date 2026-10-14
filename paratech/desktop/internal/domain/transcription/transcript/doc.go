// Package transcript is the aggregate of the text result of transcribing one recording
// (PRD sections 8 and 16, AC-06, AC-08).
//
// A transcript names the recording it came from, the model that produced it (id, name and version; the name and
// version are copies, so they stay readable after the model is removed, AC-08), the detected language and the
// transcript document below transcripts/. An STT job produces exactly one transcript, so the transcript id is the job
// id and the document lives at DocumentRef(id): a rerun of the job finds what an earlier attempt stored.
//
// Document is the stored format of the recognised text (segments with their time spans). The STT engine writes it,
// later steps (summaries, homework extraction) read it; the format is owned here, not by the engine adapter.
//
// The aggregate does no I/O and knows no time: the creation time is given by the caller.
package transcript
