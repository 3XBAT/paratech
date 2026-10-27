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

## Speech recognition component (TASK-072)

The pinned set lives in one manifest, `desktop/internal/app/speechcomponent.json` (embedded; the build scripts read the
same file). Fields: `schema_version` (1), `version` (build tag), `source_url`, `size_bytes` and `sha256` of the archive,
`download_hosts`, `entry_prefix` (`Release/`), `executable` (`whisper-cli.exe`), `vad_model` and `files`: `name`, `size_bytes`,
`sha256` of each of the 14 files (`whisper-cli.exe`, `whisper.dll`, `ggml.dll`, `ggml-base.dll`, nine `ggml-cpu-*.dll` from the archive,
and `ggml-silero-v5.1.2.bin`). A file with its own `source_url` is not in the archive and is downloaded on its own (its host must be
in `download_hosts`): the Silero VAD model (TASK-222, ADR-0030, see "Voice activity detection"). Parsed by `values.ParseSpeechComponent`.

- **Hosts.** `github.com` answers `302` to `release-assets.githubusercontent.com` (seen 2026-10-18);
  `objects.githubusercontent.com` is the older redirect host. For the VAD model `huggingface.co` and the Hugging Face CDN hosts of the
  model catalog (below). Only these, exact match.
- **Where the app looks**, in order: (a) the folder of the executable (installer, build scripts; read only), then
  (b) `<data dir>\components\speech` (the in-app install; always writable). A folder counts only when all 14 files are
  regular files (no links or reparse points) with the pinned size and SHA-256; some files but no verified set is
  "damaged". Detection hashes about 12.5 MB at start and after an install.
- **In-app install** (button «Установить компонент распознавания»): download from the hosts above, size and SHA-256 of
  the archive checked before it is opened; only the entries named exactly `Release/<file>` are unpacked, each to a path
  built from the manifest name, bounded and hashed while written, into `components\staging-*`; only a fully verified
  set replaces `components\speech`. The VAD model is downloaded after the archive (whole file, size and SHA-256 checked) into
  `components\download-<name>` and copied into the staging folder with the same checks. Nothing from the archive runs before that.
  Leftovers are removed at the next start. A folder of an older app version (13 files, no VAD model) counts as damaged: the page
  offers the install again.
  Then the engine is built from the new folder and the `stt` job type is registered at run time (no restart).
- **Refresh.** New tag: update the manifest (archive size and digest from the GitHub asset, the 13 file sizes and
  digests from the unpacked `Release/`), check `whisper-cli.exe --help` runs with only these files and still lists the VAD
  options, update `app/speech_test.go` (`TestEmbeddedSpeechManifest`).

## Voice activity detection (TASK-222, ADR-0030)

whisper-cli of b5454 has the VAD options (checked with `whisper-cli.exe --help` of the pinned binary, 2026-10-27):

```
--vad                               [false  ] enable Voice Activity Detection (VAD)
-vm FNAME, --vad-model FNAME        [       ] VAD model path
-vt N,     --vad-threshold N        [0.50   ] VAD threshold for speech recognition
-vspd N,   --vad-min-speech-duration-ms  N [250    ]
-vsd N,    --vad-min-silence-duration-ms N [100    ]
-vmsd N,   --vad-max-speech-duration-s   N [FLT_MAX]
-vp N,     --vad-speech-pad-ms           N [30     ]
-vo N,     --vad-samples-overlap         N [0.10   ]
```

The adapter passes only `--vad -vm <component folder>\ggml-silero-v5.1.2.bin`; the other options keep these defaults, and the
decoder stays the CLI default (beam search `-bs 5 -bo 5`, owner 2026-10-27).

- **Model.** `ggml-silero-v5.1.2.bin`, 885 098 bytes, SHA-256 `29940d98d42b91fbd05ce489f3ecf7c72f0a42f027e4875919a28fb4c04ea2cf`,
  from https://huggingface.co/ggml-org/whisper-vad (`resolve/main/ggml-silero-v5.1.2.bin`, downloaded 2026-10-26). **To pin**: the
  `source_url` names `main`, not a commit, and the digest was computed from the download, not compared with the `lfs.oid` of the
  Hugging Face API (no network in that session). The SHA-256 still pins the content: another file fails the install.
- **Timestamps** (pinned binary, base-q5_1, synthetic lecture of TTS phrases with pauses of 3–75 s, 2026-10-27; fixture
  `adapters/whispercli/testdata/vad`): the JSON offsets and the stdout lines are on the time line of the whole recording. Every
  passage starts within 0.4 s of where it is spoken (without VAD the second one started 19 s early and whisper invented text in a
  75 s pause). A segment before a pause **ends where the next speech starts** (one segment 52.6–132.1 s for about 4 s of speech): ends
  are stretched over the silence, starts are right and never go back. The live preview and the stored document only need ordered
  starts; the lecture text and the summary time codes use the starts.
- **Speed** (same machine, 4 threads): synthetic 217 s, base-q5_1: 29.3 s without VAD, 14.8 s with it (encoder windows 10 → 3);
  5 min of a real lecture, small-q5_1: 239.5 s → 120.9 s (research of 2026-10-26).

## Models

- Official ggml models: `https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-<name>.bin`
  (`<name>` e.g. `base`, `small`, `large-v3-turbo`, quantized `base-q5_1` etc.); the URL pattern is from `models/download-ggml-model.sh`
  (`src=https://huggingface.co/ggerganov/whisper.cpp`, `pfx=resolve/main/ggml`). Checksums: take the SHA-256 shown on the Hugging Face
  file page and record it in the catalog; verify after download (PRD section 8.1).

## Model catalog (TASK-067, TASK-070)

The embedded catalog `desktop/internal/app/catalog.json`: every size of the official repository in the full and the
quantized (q5) variant, and the English-only `.en` models (there is no `large-v3.en`). Sizes and SHA-256 (`lfs.size`,
`lfs.oid`) from the Hugging Face API https://huggingface.co/api/models/ggerganov/whisper.cpp/tree/5359861c739e955e79d9a303bcbc70fb988958b1,
first on 2026-10-16 (TASK-067, six files) and for all 20 files on **2026-10-18**, when `main` was still commit
`5359861c739e955e79d9a303bcbc70fb988958b1`; on both dates a `HEAD` of each resolve URL (`curl -sI`, redirect not followed)
returned the same `X-Linked-ETag` and `X-Linked-Size`.
`source_url` is pinned to that commit: `https://huggingface.co/ggerganov/whisper.cpp/resolve/5359861c739e955e79d9a303bcbc70fb988958b1/<file>`.
`source_page` is pinned to the same commit: `https://huggingface.co/ggerganov/whisper.cpp/blob/5359861c739e955e79d9a303bcbc70fb988958b1/<file>`.
Download of `ggml-base-q5_1.bin` through the app checked on Windows with `LA_MODEL_DOWNLOAD_LIVE_TEST=1`
(`app/catalog_live_windows_test.go`).

The full files `ggml-<family>[.en].bin` hold f16 weights; their catalog id and name are `<family>[.en]-f16`, so plain names
like `small` stay free for imported models. The ids of the six TASK-067 entries are unchanged (installed models stay
recognized). Order: multilingual by size (quantized before full within a family), then English the same way.

| File | Id | Bytes | SHA-256 | Family | Variant | Tier | Load |
|---|---|---|---|---|---|---|---|
| `ggml-tiny-q5_1.bin` | `tiny-q5_1` | 32152673 | `818710568da3ca15689e31a743197b520007872ff9576237bda97bd1b469c3d7` | tiny | quantized (q5_1) | fast | 1 |
| `ggml-tiny.bin` | `tiny-f16` | 77691713 | `be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21` | tiny | full (f16) | fast | 1 |
| `ggml-base-q5_1.bin` | `base-q5_1` | 59707625 | `422f1ae452ade6f30a004d7e5c6a43195e4433bc370bf23fac9cc591f01a8898` | base | quantized (q5_1) | fast | 1 |
| `ggml-base.bin` | `base-f16` | 147951465 | `60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe` | base | full (f16) | fast | 2 |
| `ggml-small-q5_1.bin` (preferred) | `small-q5_1` | 190085487 | `ae85e4a935d7a567bd102fe55afc16bb595bdb618e11b2fc7591bc08120411bb` | small | quantized (q5_1) | balanced | 2 |
| `ggml-small.bin` | `small-f16` | 487601967 | `1be3a9b2063867b937e64e2ec7483364a79917e157fa98c5d94b5c1fffea987b` | small | full (f16) | balanced | 3 |
| `ggml-medium-q5_0.bin` | `medium-q5_0` | 539212467 | `19fea4b380c3a618ec4723c3eef2eb785ffba0d0538cf43f8f235e7b3b34220f` | medium | quantized (q5_0) | accurate | 3 |
| `ggml-medium.bin` | `medium-f16` | 1533763059 | `6c14d5adee5f86394037b4e4e8b59f1673b6cee10e3cf0b11bbdbee79c156208` | medium | full (f16) | accurate | 4 |
| `ggml-large-v3-turbo-q5_0.bin` | `large-v3-turbo-q5_0` | 574041195 | `394221709cd5ad1f40c46e6031ca61bce88931e6e088c188294c6d5a55ffa7e2` | large-v3-turbo | quantized (q5_0) | max | 4 |
| `ggml-large-v3-turbo.bin` | `large-v3-turbo-f16` | 1624555275 | `1fc70f774d38eb169993ac391eea357ef47c88757ef72ee5943879b7e8e2bc69` | large-v3-turbo | full (f16) | max | 5 |
| `ggml-large-v3-q5_0.bin` | `large-v3-q5_0` | 1081140203 | `d75795ecff3f83b5faa89d1900604ad8c780abd5739fae406de19f23ecd98ad1` | large-v3 | quantized (q5_0) | max | 4 |
| `ggml-large-v3.bin` | `large-v3-f16` | 3095033483 | `64d182b440b98d5203c4f9bd541544d84c605196c4f7b845dfa11fb23594d1e2` | large-v3 | full (f16) | max | 5 |
| `ggml-tiny.en-q5_1.bin` (English) | `tiny.en-q5_1` | 32166155 | `c77c5766f1cef09b6b7d47f21b546cbddd4157886b3b5d6d4f709e91e66c7c2b` | tiny | quantized (q5_1) | fast | 1 |
| `ggml-tiny.en.bin` (English) | `tiny.en-f16` | 77704715 | `921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f` | tiny | full (f16) | fast | 1 |
| `ggml-base.en-q5_1.bin` (English) | `base.en-q5_1` | 59721011 | `4baf70dd0d7c4247ba2b81fafd9c01005ac77c2f9ef064e00dcf195d0e2fdd2f` | base | quantized (q5_1) | fast | 1 |
| `ggml-base.en.bin` (English) | `base.en-f16` | 147964211 | `a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002` | base | full (f16) | fast | 2 |
| `ggml-small.en-q5_1.bin` (English) | `small.en-q5_1` | 190098681 | `bfdff4894dcb76bbf647d56263ea2a96645423f1669176f4844a1bf8e478ad30` | small | quantized (q5_1) | balanced | 2 |
| `ggml-small.en.bin` (English) | `small.en-f16` | 487614201 | `c6138d6d58ecc8322097e0f987c32f1be8bb0a18532a3f88f734d1bbf9c41e5d` | small | full (f16) | balanced | 3 |
| `ggml-medium.en-q5_0.bin` (English) | `medium.en-q5_0` | 539225533 | `76733e26ad8fe1c7a5bf7531a9d41917b2adc0f20f2e4f5531688a8c6cd88eb0` | medium | quantized (q5_0) | accurate | 3 |
| `ggml-medium.en.bin` (English) | `medium.en-f16` | 1533774781 | `cc37e93478338ec7700281a7ac30a10128929eb8f427dda2e865faa8f6da4356` | medium | full (f16) | accurate | 4 |

**Load on the computer** (`values.LoadOf`, 1..5). The level of the full variant follows the parameter count: tiny (39M) 1,
base (74M) 2, small (244M) 3, medium (769M) 4, large-v3 (1550M) 5, large-v3-turbo (809M) 5: turbo has only 4 decoder
layers but the full large-v3 encoder, and the encoder takes most of the CPU time on long recordings. A quantized variant
(quantization `q…`) is one step lower, at least 1: the file is 2.4–2.9 times smaller, it needs less memory and runs
a bit faster on a CPU, with slightly lower accuracy. A model without a family has load 0 (unknown). Loads 4 and 5 are
heavy (`ModelLoad.Heavy`): the page warns that recognition will be long on a usual processor. The core sends
`heavy` and `reduced` (quantized, `Quantization.Reduced`) with each catalog item, so the page does not repeat the rules. The words for the levels
are UI text and live in the frontend. The rule is qualitative until RTF is measured on real machines (PRD section 8.1).

**Download hosts.** `huggingface.co` answers `302` to a CDN; the allowlist matches exact hosts, so `download_hosts` lists
`us.aws.cdn.hf.co` (seen on 2026-10-16), `cas-bridge.xethub.hf.co`, `transfer.xethub.hf.co`, `cdn-lfs.hf.co`, `cdn-lfs-us-1.hf.co`,
`cdn-lfs-eu-1.hf.co`. Not `cas-bridge-direct.xethub.hf.co`: it resolves to a private address. A redirect to a new CDN host fails the
download with "Источник модели ответил не так, как ожидалось" (`values.ErrHostNotAllowed`); then add the host after checking it
(`curl -sI <resolve URL>` shows the `Location`) and run the live test.

**Refresh.** Take `lfs.oid`/`lfs.size` from the API above for a new commit, update `source_url`, `source_page`, the table and
`app/catalog_test.go`.

## Command line used by the adapter

```
whisper-cli.exe -m <model> -f <audio> -of <run-dir>\out -oj -pp -ng -t <threads> -l <lang> --vad -vm <component>\ggml-silero-v5.1.2.bin
```

| Flag | Long form | Meaning (cli.cpp v1.9.5) | Adapter value |
|---|---|---|---|
| `-m FNAME` | `--model` | model path (default `models/ggml-base.en.bin`) | absolute path below `<data>/models` |
| `-f FNAME` | `--file` | input audio path (repeatable) | absolute path below `<data>/recordings` |
| `-of FNAME` | `--output-file` | output path **without extension**; `-` means stdout | `<work>/run-XXXX/out` |
| `-oj` | `--output-json` | write `<of>.json` | always |
| `-pp` | `--print-progress` | progress lines on stderr | always |
| `-ng` | `--no-gpu` | disable GPU | always (CPU-only, PRD section 4.3) |
| `-t N` | `--threads` | threads (default `min(4, hardware threads)`) | load mode (ADR-0016), 1..256 |
| `-l LANG` | `--language` | language code or `auto` (default `en`); lower-cased by the CLI | config, allowlist below |
| `--vad` | | voice activity detection: only speech is recognised | when the component has a VAD model (always for the shipped one) |
| `-vm FNAME` | `--vad-model` | VAD model path | absolute path in the verified component folder (next to the program); a file that cannot be used is logged and the run goes on without `--vad` |

Other verified flags, not used: `-p N` processors, `-ot/-on/-d` offsets/duration, `-mc`, `-ml`, `-sow`, `-bo`, `-bs`, `-ac`, `-wt`, `-et`,
`-lpt`, `-nth`, `-tp`, `-tpi`, `-debug`, `-tr` translate, `-di`, `-tdrz`, `-nf`, `-otxt`, `-ovtt`, `-osrt`, `-olrc`, `-owts`, `-fp`, `-ocsv`,
`-ojf` (JSON with tokens; implies `-oj`), `-np` no prints, `-ps`, `-pc`, `--print-confidence`, `-nt` no timestamps, `-dl` detect language
and exit, `--prompt`, `--carry-initial-prompt`, `-oved`, `-dtw`, `-ls`, `-dev N`, `-fa`/`-nfa` flash attention (default on), `-sns`,
`--suppress-regex`, `--grammar`, `--grammar-rule`, `--grammar-penalty`, `-vt`, `-vspd`, `-vsd`, `-vmsd`, `-vp`, `-vo`,
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

### Stdout (live segments, TASK-079)

`whisper_print_segment_callback` (cli.cpp, set as `new_segment_callback` unless `--print-realtime`) prints every new
segment as soon as it is decoded, then `fflush(stdout)`:

```

[00:00:00.000 --> 00:00:05.000]   Recognised text.
```

Format: `printf("[%s --> %s]  ", to_timestamp(t0), to_timestamp(t1))`, then the segment text as is (it starts with a
space), then `\n`; a single `\n` before the first segment. `to_timestamp` is `%02d:%02d:%02d%s%03d` (hours, minutes,
seconds, `.`, milliseconds; t is in 10 ms units, the same values as `offsets` of the JSON). Both format strings are in
the official `whisper-cli.exe` of b5454 (the pinned SHA-256 of `speechcomponent.json`; checked with `grep -a` on the binary, 2026-10-18). The text bytes are UTF-8
from the model (never the ANSI code page); on Windows the C runtime writes `\r\n`. `-np` does not turn it off.

The adapter reads stdout only when the caller passes a segment callback (the STT job stores the live transcript),
otherwise stdout goes to the null device. The parser keeps one line of at most 16 KiB + 64 bytes; it accepts only
lines matching `^\[hh:mm:ss.mmm --> hh:mm:ss.mmm\](text)$` (2–3 hour digits), clamps times like the JSON (start not
before the previous start, end not before start, at most 24 h), turns invalid UTF-8 into U+FFFD and control characters
into spaces, trims, drops empty texts and everything else. Segments go to the callback on their own goroutine through a
bounded queue (4096 segments, 8 MiB; beyond that they are dropped), so a slow callback never blocks the pipe. The text
is the transcript itself: it never reaches logs or errors. The JSON result stays the source of truth.

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
