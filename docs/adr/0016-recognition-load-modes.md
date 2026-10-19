# ADR-0016: Recognition load modes and time estimates

- **Status:** Accepted
- **Date:** 2026-10-19
- **Source:** TASK-084; ADR-0001 (whisper-cli sidecar), PRD §8.

## Context
whisper-cli took 60–70 % of the CPU. The owner wants it limited (about 40 %) by default, three modes, and a concrete
number instead of vague words: «запись 1 ч ≈ N мин в этом режиме», for this computer.

## Decision
**Setting** «Нагрузка при распознавании» (`settings.recognition_load`, default `gentle`; an unknown stored value reads
as `gentle`). It is read when a recognition run starts, so a change applies from the next run; the running process keeps
its mode (changing a live job object or priority is possible but not worth the risk).

**Policy** — one place, `domain/transcription/services/loadpolicy` (n = logical processors):

| Mode | Threads (`-t`) | Priority | CPU cap |
|---|---|---|---|
| gentle «Бережная» | max(1, min(n/2, 4)) | below normal | 40 % of all processors |
| normal «Обычная» | min(4, n) (as before) | below normal | none |
| max «Максимальная» | n | normal | none |

Gentle is capped at the normal thread count, so above 8 processors it never loads more than normal.

**Mechanism** (whispercli, Windows only): `BELOW_NORMAL_PRIORITY_CLASS` in `CreationFlags`; the cap is
`JobObjectCpuRateControlInformation` with `ENABLE | HARD_CAP` and `CpuRate = 4000` on the job object that already kills
the process tree. If the cap cannot be set the run goes on uncapped with the low priority and a warning is logged. The
job is closed with the run as before. Other OSes only get the threads.

**Estimate** (`services/speedestimate`): processing time of 1 h of audio. Table factor (time / audio) of a full f16
model with 4 threads on a usual laptop CPU with AVX2 — rough, shown as «примерно»:

| tiny | base | small | medium | large-v3-turbo | large-v3 |
|---|---|---|---|---|---|
| 0.05 | 0.10 | 0.30 | 0.80 | 0.60 | 1.60 |

Quantized (q5, q8) × 0.8. Scaled by `(4 / cores)^0.7` (sublinear), where cores = threads, and for gentle at most 40 %
of n. Example, 6 processors, gentle: large-v3 ≈ 2 h 17 min, medium ≈ 1 h 9 min, small ≈ 26 min. A model outside the
catalog has no table entry: only a measurement gives a number.

**Refinement:** after a successful run of a recording of at least 1 min, factor = wall time / audio duration goes into
`stt_speeds` (per installed model and mode, deleted with the model) as a moving average (weight 0.3) with a run count.
Factors outside 0.005–50, and runs 3× off an average of 3+ runs (sleep, clock jump), are ignored. A measured mode shows
`measured` instead of `table`.

**Heavy:** more than 1 h per 1 h of audio — the page warns with the number.

**Time left** of a recording: queued — the whole estimate; running — estimate × (1 − job progress %), before the first
progress the estimate less the time since the start; never below 1 min.

## Consequences
- Gentle makes recognition about 1.2–1.5× slower than before on 6–8 processors; the user sees the number.
- The table is a guess until the first runs; the owner checks it (docs/qa/windows-checklist.md).
- New migration 0010 (column + table); `STTEngine.Transcribe` takes the mode.
