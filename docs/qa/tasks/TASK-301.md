# TASK-301 — check steps

1. Open «Записи и задачи» -> «Импортировать запись» (any lecture) and choose a WAV with a large metadata block (LIST/JUNK over 64 KiB) before the audio, e.g. one tagged by an audio editor with a cover or long notes. Expected: the file is accepted, the recording appears with the right duration and recognition starts.
2. Choose a WAV that has no audio chunk (a cut or damaged file). Expected: «Не удалось узнать длительность записи в этом WAV-файле. …» (the text says WAV, not «WAV или MP3»).
3. Same for an MP3 with no readable frames: the text says «в этом MP3-файле»; FLAC and OGG likewise.
4. Regular WAV/MP3 imports behave as before.
