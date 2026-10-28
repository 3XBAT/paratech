-- Three teachers of a subject by role (TASK-285, ADR-0030 «Teachers of a subject»): lecture, practice, lab.
--
-- The teacher of TASK-235 (subjects.teacher_*) moves into all three roles: every send of the subject went to that
-- address, so it still does. subjects.teacher_* are then blanked and no longer read; they stay as legacy columns
-- (SQLite cannot drop them because of the shared CHECK). One UPDATE blanks both, so that CHECK holds on every row.
-- Down puts one teacher back (lecture, else practice, else lab) and drops the table.
--
-- docs/contracts/schema.sql mirrors the CREATE TABLE below VERBATIM; the moves of rows are not part of the contract
-- (TestMigrationMatchesContractSchema).

-- +goose Up
-- subject_teachers: the teachers of a subject (subject.Teachers), part of the subject aggregate (no version of their
-- own): one row per role that has a teacher. Personal data typed by the student, not a secret; never sent to the
-- agent or the Hub (ADR-0022).
CREATE TABLE subject_teachers (
    subject_id TEXT NOT NULL REFERENCES subjects (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('lecture', 'practice', 'lab')),
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    email      TEXT NOT NULL CHECK (length(email) BETWEEN 1 AND 254),
    PRIMARY KEY (subject_id, role)
);

INSERT INTO subject_teachers (subject_id, role, name, email)
SELECT s.id, r.role, s.teacher_name, s.teacher_email
FROM subjects s
CROSS JOIN (SELECT 'lecture' AS role UNION ALL SELECT 'practice' UNION ALL SELECT 'lab') r
WHERE s.teacher_name <> '' AND s.teacher_email <> '';

UPDATE subjects SET teacher_name = '', teacher_email = '' WHERE teacher_name <> '' OR teacher_email <> '';

-- +goose Down
UPDATE subjects
SET teacher_name  = (SELECT t.name FROM subject_teachers t WHERE t.subject_id = subjects.id
                     ORDER BY CASE t.role WHEN 'lecture' THEN 0 WHEN 'practice' THEN 1 ELSE 2 END LIMIT 1),
    teacher_email = (SELECT t.email FROM subject_teachers t WHERE t.subject_id = subjects.id
                     ORDER BY CASE t.role WHEN 'lecture' THEN 0 WHEN 'practice' THEN 1 ELSE 2 END LIMIT 1)
WHERE id IN (SELECT subject_id FROM subject_teachers);

DROP TABLE subject_teachers;
