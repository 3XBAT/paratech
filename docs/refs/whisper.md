# whisper-cli (whisper.cpp) sidecar

Version: whisper.cpp **v1.9.5** (tag commit `d1be6fde`, released 2026-10-06), verified on 2026-10-13 against the source of that tag.
Sources:
- CLI source (flags, exit codes, JSON writer): https://github.com/ggml-org/whisper.cpp/blob/v1.9.5/examples/cli/cli.cpp
- CLI help: https://github.com/ggml-org/whisper.cpp/blob/v1.9.5/examples/cli/README.md
- Progress callback and language list: https://github.com/ggml-org/whisper.cpp/blob/v1.9.5/src/whisper.cpp (`g_lang`, `progress_callback`)
- Audio decoding: https://github.com/ggml-org/whisper.cpp/blob/v1.9.5/examples/common-whisper.cpp (`read_audio_data`)
- Backend loading: https://github.com/ggml-org/whisper.cpp/blob/v1.9.5/ggml/src/ggml-backend-reg.cpp
- Releases: https://github.com/ggml-org/whisper.cpp/releases (GitHub API `repos/ggml-org/whisper.cpp/releases`)
- Models: https://huggingface.co/ggerganov/whisper.cpp (README "Ggml format"; `models/download-ggml-model.sh`)

Used by `desktop/internal/adapters/whispercli` (TASK-024). Re-verify everything below when the pinned version changes.

## Official Windows binaries

- Version tags (`v1.9.5`) have **no assets**. The binaries are attached to the nightly build tag named in the release notes:
  v1.9.5 -> **`b5454`** (same commit `d1be6fde`), https://github.com/ggml-org/whisper.cpp/releases/tag/b5454
- CPU build for x64 Windows: `whisper-bin-x64.zip`, 8 928 640 bytes,
  SHA-256 `6ba69e3482d7826214f90a6a9c84ca07782aec1e1d0c6a7c30c994fd5d816ccb`
  (GitHub asset digest; re-computed locally from the download, matches).
  Other Windows assets: `whisper-bin-Win32.zip` (x86), `whisper-bin-win-cpu-arm64.zip`, `whisper-blas-bin-x64.zip` (OpenBLAS),
  CUDA and OpenCL variants (not used: CPU-only in MVP, PRD section 4.3).
- Contents of `whisper-bin-x64.zip` (folder `Release/`) needed at run time: `whisper-cli.exe`, `whisper.dll`, `ggml.dll`, `ggml-base.dll`
  and the CPU backend variants `ggml-cpu-*.dll` (x64, sse42, sandybridge, haswell, skylakex, cannonlake, cascadelake, icelake, alderlake),
  which ggml picks at run time for the CPU. The zip also has `main.exe` (deprecated name), `whisper-server.exe`, `whisper-stream.exe`,
  tests and other examples, which the app does not need. Ship the DLLs next to `whisper-cli.exe`.
- The executable's manifest has no `activeCodePage` (UTF-8) entry and the CLI uses `main(argc, argv)`, so on Windows **arguments arrive in
  the system ANSI code page** (see "Windows argument encoding").
- Pinning: the app pins the release by the zip SHA-256 above (and the build tag); the model is pinned by file + SHA-256 in the model
  catalog (PRD section 8.1, TASK-026).

## Models

- Official ggml models: `https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-<name>.bin`
  (`<name>` e.g. `base`, `small`, `large-v3-turbo`, quantized `base-q5_1` etc.); the URL pattern is from `models/download-ggml-model.sh`
  (`src=https://huggingface.co/ggerganov/whisper.cpp`, `pfx=resolve/main/ggml`). Checksums: take the SHA-256 shown on the Hugging Face
  file page and record it in the catalog; verify after download (PRD section 8.1).

## Command line used by the adapter

```
whisper-cli.exe -m <model> -f <audio> -of <run-dir>\out -oj -pp -ng -t <threads> -l <lang>
```

| Flag | Long form | Meaning (cli.cpp v1.9.5) | Adapter value |
|---|---|---|---|
| `-m FNAME` | `--model` | model path (default `models/ggml-base.en.bin`) | absolute path below `<data>/models` |
| `-f FNAME` | `--file` | input audio path (repeatable) | absolute path below `<data>/recordings` |
| `-of FNAME` | `--output-file` | output path **without extension**; `-` means stdout | `<work>/run-XXXX/out` |
| `-oj` | `--output-json` | write `<of>.json` | always |
| `-pp` | `--print-progress` | progress lines on stderr | always |
| `-ng` | `--no-gpu` | disable GPU | always (CPU-only, PRD section 4.3) |
| `-t N` | `--threads` | threads (default `min(4, hardware threads)`) | config, 1..256 |
| `-l LANG` | `--language` | language code or `auto` (default `en`); lower-cased by the CLI | config, allowlist below |

Other verified flags, not used: `-p N` processors, `-ot/-on/-d` offsets/duration, `-mc`, `-ml`, `-sow`, `-bo`, `-bs`, `-ac`, `-wt`, `-et`,
`-lpt`, `-nth`, `-tp`, `-tpi`, `-debug`, `-tr` translate, `-di`, `-tdrz`, `-nf`, `-otxt`, `-ovtt`, `-osrt`, `-olrc`, `-owts`, `-fp`, `-ocsv`,
`-ojf` (JSON with tokens; implies `-oj`), `-np` no prints, `-ps`, `-pc`, `--print-confidence`, `-nt` no timestamps, `-dl` detect language
and exit, `--prompt`, `--carry-initial-prompt`, `-oved`, `-dtw`, `-ls`, `-dev N`, `-fa`/`-nfa` flash attention (default on), `-sns`,
`--suppress-regex`, `--grammar`, `--grammar-rule`, `--grammar-penalty`, `--vad`, `-vm`, `-vt`, `-vspd`, `-vsd`, `-vmsd`, `-vp`, `-vo`,
`-h`, `--version`.

### Argument parsing rules (security relevant)

- There is **no `--` end-of-options marker**: `--` is an unknown argument (prints usage, exits **0**).
- A value flag takes the **next argument verbatim** (`ARGV_NEXT`), even if it starts with `-`. Any argument that does not start with `-`,
  and `-` itself, is an **extra input file** (positional). `-f -` / positional `-` read audio from stdin; `-of -` writes to stdout.
- If the **only** argument starts with `@`, the rest is a **response file** read line by line as arguments.
- The adapter passes only absolute paths (never `-` or `@...`), rejects values with control characters or a leading `-`/`@`,
  and uses no shell (`os/exec`, `.exe` required on Windows so `cmd.exe` is never involved).

### Windows argument encoding

`whisper-cli.exe` receives its command line converted to the ANSI code page (`GetACP`, e.g. 1251 or 1252). Characters without an exact
mapping are replaced or "best-fit" mapped (for example U+FF02 FULLWIDTH QUOTATION MARK to `"`), which can split or merge arguments
(the "WorstFit" class), and a double-byte trail byte `0x5C` looks like `\`. File opens (`fopen`, `std::ofstream`) use the same narrow
paths. The adapter therefore accepts on Windows only arguments that round-trip exactly through the ANSI code page (no best fit, same
number of `\` and `"`); UTF-8 as the system code page (65001) accepts everything. Consequence: a data root or file name with characters
outside the code page (for example Cyrillic on a 1252 system) is rejected with `ErrInvalidInput` (`ErrConfig` for the data root itself).
Whether the app uses an ASCII-only data root, or lets the user configure one, is a product decision for TASK-025. **To verify on real
Windows** (S-003).

The same applies to output: stderr carries paths and messages as ANSI code page bytes. The adapter redacts the ANSI forms of the data
root and binary directory in the raw bytes, decodes the tail from the ANSI code page to UTF-8 and then redacts again case-insensitively,
so a non-ASCII profile name (for example Cyrillic under code page 1251) does not leak into `RunError.Stderr`.

## Environment and working directory

- Variables read: `WHISPER_ARG_DEVICE` (GPU device), `GGML_BACKEND_PATH` (loads an extra backend DLL), `GGML_DISABLE_VULKAN`, and ggml
  backend variables. The adapter passes an **empty environment** (Go adds only `SYSTEMROOT` on Windows).
- `ggml_backend_load_all()` searches backend DLLs in the executable directory **and the current directory**. The adapter sets the working
  directory to a fresh private run directory (mode 0700) below the data root. The mode has no effect on Windows; there the ACL of the
  data root protects the run directories.

## Exit codes (cli.cpp v1.9.5)

| Code | Cause |
|---|---|
| 0 | success, **but also** `-h`, `--version`, unknown argument, missing flag value, unknown language, `-di` with `-tdrz` (usage printed) |
| 1 | response file not found / parse failure |
| 2 | no usable input file (all inputs missing) |
| 3 | model file not found, model failed to load, unknown DTW preset |
| 4 | grammar could not be parsed |
| 10 | `whisper_full_parallel` failed (recognition) |
| 11 | an input could not be read or its output could not be written |

Because 0 does not prove success, the adapter treats "exit 0 without `<of>.json`" as `ErrNoOutput`.

## Output formats

### Progress (stderr, with `-pp`)

`whisper_print_progress_callback` prints every 5 % (`progress_step = 5`):

```
whisper_print_progress_callback: progress =   5%
```

Format string: `"%s: progress = %3d%%\n"` with `__func__`. The adapter parses only lines matching
`^<identifier>: progress = *<0..100>%$`, reports increasing values (at most once a second) and keeps 100 for after the result is stored.
The callback runs on its own goroutine fed with the latest value, so a slow callback never blocks the stderr pipe (a full pipe would
stall whisper-cli); it must be goroutine-safe.
Other stderr content: system info, model loading logs, `main: processing '<audio path>' ...`, `output_json: saving output to '<path>'`,
timings, and error messages (`error: ...`), all with full paths: the adapter redacts the data root and binary directory.

### Stdout

Each new segment is printed as `[00:00:00.000 --> 00:00:05.000]  text` (also with `-np`). It carries the transcript text, so the adapter
discards stdout.

### JSON (`-oj`, file `<of>.json`)

Written by `output_json` with tab indentation:

```json
{
	"systeminfo": "AVX = 1 | ...",
	"model": {"type": "base", "multilingual": true, "vocab": 51865, "audio": {...}, "text": {...}, "mels": 80, "ftype": 1},
	"params": {"model": "<model path>", "language": "auto", "translate": false},
	"result": {"language": "ru"},
	"transcription": [
		{
			"timestamps": {"from": "00:00:00,000", "to": "00:00:05,000"},
			"offsets": {"from": 0, "to": 5000},
			"text": " Recognised text."
		}
	]
}
```

- `offsets` are integer milliseconds (`t * 10`, whisper time unit is 10 ms); the adapter uses them, not the `timestamps` strings.
  Negative values and values above 24 h fail the result; an end before its start or a start before the previous start is clamped
  (end raised to start, start raised to the previous start), so one odd segment does not fail a lecture.
- Strings escape **only `"` and `\`**: a raw newline, tab or other control byte in text makes the file invalid JSON. The adapter turns raw
  control bytes into spaces before a strict parse.
- `-ojf` adds `tokens` per segment; `-di`/`-tdrz` add `speaker`/`speaker_turn_next`. Not used.
- Segment text starts with a space; invalid UTF-8 can occur at token boundaries (Go's decoder replaces it with U+FFFD).

## Audio input

`read_audio_data` decodes with miniaudio (WAV, FLAC, MP3, OGG) to 16 kHz float PCM (mono, or stereo with `-di`); other sample rates are
resampled. The README still says "only 16-bit WAV"; the source (miniaudio decoder) is authoritative for v1.9.5. To verify with the
recorder's real chunk format (TASK-017).

## Languages (`-l`)

`auto` plus the 100 codes of `g_lang`: en zh de es ru ko fr ja pt tr pl ca nl ar sv it id hi fi vi he uk el ms cs ro da hu ta no th ur hr bg
lt la mi ml cy sk te fa lv bn sr az sl kn et mk br eu is hy ne mn bs kk sq sw gl mr pa si km sn yo so af oc ka be tg sd gu am yi lo uz fo ht
ps tk nn mt sa lb my bo tl mg as tt haw ln ha ba jw su yue.

## Cancellation

The CLI has no cancel signal (its abort callbacks are stubs that never fire). The adapter kills the process: on Windows through a job
object (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, `TerminateJobObject`; also kills the tree if this app crashes), elsewhere with
`os.Process.Kill`. Partial `<of>.json` files stay in the private run directory, which is removed.

## Open points (adapter side)

- **Paths resolver**: the adapter's `Paths` port (`Path(ctx, ref)`) is not implemented anywhere yet. The wiring task must add it to the
  filestore adapter and check every path component for links (symlinks, junctions): the adapter only `Lstat`s the last element.
- **Transcript format**: the stored transcript JSON (`Transcript`, `TranscriptFormatVersion`) is owned by the adapter for now; TASK-029
  must move it into a domain value.
- **Non-ASCII data roots** outside the ANSI code page are rejected on Windows (see "Windows argument encoding"); product decision for
  TASK-025.
- **Run directory mode** 0700 is meaningless on Windows; protection is the data root's ACL.
