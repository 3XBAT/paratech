-- The knowledge base (TASK-091): no index of its own, it reads the recordings and what was made from them, so a
-- deleted recording leaves it with its rows (ADR-0018, ADR-0019). Lessons are completed recordings with a track that
-- is not silent and no deletion mark (recording.Recording.InKnowledgeBase decides; the filter only saves reads).

-- Ids only, so that one row that cannot be read does not hide the others (the caller loads each).
-- name: ListKnowledgeBaseRecordingIDs :many
SELECT id FROM recordings
WHERE id IS NOT NULL
  AND status = sqlc.arg(status)
  AND audio_ref IS NOT NULL
  AND silent = 0
  AND deleting_at IS NULL
ORDER BY started_at DESC, id DESC;

-- What is stored for the newest transcript of each lesson (the order of LatestTranscriptByRecording): its id (empty: none),
-- whether it has a summary and a finished lecture text (blocks are empty until a run completed).
-- name: ListKnowledgeBaseReadiness :many
SELECT r.id AS recording_id,
       CAST(COALESCE(t.id, '') AS TEXT) AS transcript_id,
       CAST(EXISTS (SELECT 1 FROM summaries s WHERE s.id = t.id) AS INTEGER) AS has_summary,
       CAST(EXISTS (SELECT 1 FROM lecture_texts l WHERE l.id = t.id AND json_array_length(l.blocks) > 0) AS INTEGER) AS has_lecture_text
FROM recordings r
LEFT JOIN transcripts t ON t.id = (SELECT t2.id FROM transcripts t2
                                   WHERE t2.recording_id = r.id
                                   ORDER BY t2.created_at DESC, t2.id DESC
                                   LIMIT 1)
WHERE r.id IS NOT NULL
  AND r.status = sqlc.arg(status)
  AND r.silent = 0
  AND r.deleting_at IS NULL;
