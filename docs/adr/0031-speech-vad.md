# ADR-0031: Skipping silence in speech recognition (Silero VAD)

- **Status:** Accepted
- **Date:** 2026-10-27
- **Source:** TASK-222; research `paratech-ops/research/stt-performance-2026-10.md`; ADR-0001 (whisper-cli sidecar), ADR-0016.

## Context
A 1 h lecture took about 1 h to recognise (small-q5_1, «Бережная»). With voice activity detection whisper-cli recognises
only the speech, in fewer 30 s windows: ×2 on 5 min of a real lecture and on synthetic audio with long pauses. The owner
(2026-10-27) allowed only this: the decoder (`-bs`/`-bo`), the 40 % cap and the thread counts stay as they are.

## Decision
- **Flags.** Every run gets `--vad -vm <component folder>\ggml-silero-v5.1.2.bin`; the other VAD options keep the
  defaults of whisper-cli b5454 (docs/refs/whisper.md, "Voice activity detection"). A VAD file that cannot be used
  (not a regular file, a path the ANSI command line would change) is logged and the runs go on without VAD.
- **New dependency (a file).** The Silero v5.1.2 model of whisper.cpp, 885 098 bytes, from
  `huggingface.co/ggml-org/whisper-vad`, pinned by size and SHA-256. It is part of the speech component: a manifest
  file with a `source_url` of its own is downloaded on its own (not from the archive), `vad_model` names it. The
  installer and the in-app install ship it; a component folder without it is damaged, so a folder installed by an
  older version is installed again from the page.
- **Estimate.** The table of ADR-0016 is recalibrated on the measurements of 2026-10-26 (whisper-cli defaults, no
  VAD, 4 threads): tiny 0.17, base 0.35, small 1.0, medium 2.7, large-v3-turbo 2.0, large-v3 5.3 (f16; quantized
  × 0.8), then × 0.6 for the skipped silence: 0.5 was measured on a real lecture, 0.94 on synthetic speech with
  pauses under 2 s, so the estimate errs to the longer side until the first runs here are measured. Example,
  8 processors, gentle: small-q5_1 ≈ 34 min per hour.
- **Old measurements.** `stt_speeds` measured without VAD would show twice the time: migration 0037 deletes them;
  the table applies until the next runs. A later change of the arguments that changes the speed does the same.
- **Time codes.** With VAD the starts stay on the time line of the recording and in order; a segment before a pause
  ends where the next speech starts. The live preview, the document and the lecture text use the starts, so nothing
  changes there (fixture test `adapters/whispercli/testdata/vad`).

## Consequences
- Recognition up to twice as fast, the more pauses the more; on almost continuous speech only a few percent. Short
  phrases in noise could be lost (not seen on the samples).
- Segment ends are not where the speech ends; nothing may rely on them for the length of speech. The transcript view
  caps the end by an estimate of the spoken length of the text before looking for a pause.
- The estimate assumes VAD is on; without the model file the engine runs without it, and measurements then correct it.
- Install downloads ~0.9 MB more from a second host family (Hugging Face).
- Open: pin `source_url` to a commit of `ggml-org/whisper-vad` and compare the SHA-256 with its `lfs.oid`.
