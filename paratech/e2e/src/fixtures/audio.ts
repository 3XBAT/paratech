// Audio fixtures of the recording pack (TASK-378): variants of the committed clip made at run time (the app answers a file
// it has seen before with the EXISTING recording, so the cancel and restart scenarios need other bytes), the expected words
// of the fixture README, the speed line of the report and the AC-06 scan of what the Hub sent to the provider.
import path from "node:path";

import type { RecordedRequest } from "../fakellm/index.js";

/** The committed clip: MPEG-2 Layer III (MP3), 64 kbps CBR, 22.05 kHz mono; ~50 s (fixtures/audio/README.md). */
export const ClipRel = "audio/chekhov-chelovek-v-futliare-s1-20-70s.mp3";
export const ClipBytesPerSecond = 8_000; // 64 kbps

/** A syncsafe integer: 4 bytes of 7 bits (ID3v2 sizes). */
function syncsafe(n: number): Buffer {
  return Buffer.from([(n >> 21) & 0x7f, (n >> 14) & 0x7f, (n >> 7) & 0x7f, n & 0x7f]);
}

/**
 * An ID3v2.3 tag with one TIT2 (title) frame holding `label`. A player ignores it; the app's DetectAudio skips it (the ID3
 * branch) and the copy and the digest include it, so a clip behind another label is another file.
 */
export function id3Tag(label: string): Buffer {
  if (!/^[A-Za-z0-9 ._-]{1,64}$/.test(label)) {
    throw new Error(`id3Tag: label ${JSON.stringify(label)} is not plain ASCII`);
  }
  const text = Buffer.concat([Buffer.from([0x00]), Buffer.from(label, "latin1")]); // encoding 0 = ISO-8859-1
  const size = Buffer.alloc(4);
  size.writeUInt32BE(text.length); // ID3v2.3 frame sizes are plain big-endian
  const frame = Buffer.concat([Buffer.from("TIT2", "latin1"), size, Buffer.from([0x00, 0x00]), text]);
  return Buffer.concat([Buffer.from("ID3", "latin1"), Buffer.from([0x03, 0x00, 0x00]), syncsafe(frame.length), frame]);
}

/** The clip with a tag in front, repeated `times` (a longer recording of the same speech). Not committed anywhere. */
export function audioVariant(clip: Buffer, label: string, times = 1): Buffer {
  if (!Number.isInteger(times) || times < 1) {
    throw new Error(`audioVariant: times ${times}`);
  }
  return Buffer.concat([id3Tag(label), ...Array.from({ length: times }, () => clip)]);
}

/** Seconds of audio in `bytes` of the clip's CBR stream (the tag is not counted). */
export function clipSeconds(clipBytes: number, times = 1): number {
  return (clipBytes * times) / ClipBytesPerSecond;
}

/** "Expected words: a, b, c" of the README of the fixture, lower-cased. */
export function expectedWords(readme: string): string[] {
  const line = readme.split(/\r?\n/).find((l) => /^\s*(?:[-*]\s*)?\**Expected words\**:/i.test(l));
  if (line === undefined) {
    return [];
  }
  return line
    .replace(/^[^:]*:\**/, "")
    .split(/[,;]/)
    .map((w) => w.replace(/[`*«»"'.]/g, "").trim().toLowerCase())
    .filter((w) => w !== "");
}

/** The words of `expected` that occur in `text`, case-insensitively (ё = е: the recognizer is not consistent about it). */
export function foundWords(text: string, expected: readonly string[]): string[] {
  const fold = (s: string) => s.toLowerCase().replace(/ё/g, "е");
  const hay = fold(text);
  return expected.filter((w) => hay.includes(fold(w)));
}

/**
 * The model in a whisper-cli command line: the app copies it as `ggml-<id>.bin` or `<id>-<hash>.bin` (artifacts\models\), and
 * the file is passed by path, so this looks for the id in the last path segment and not at the whole line.
 */
export function modelFileInCommand(id: string): RegExp {
  const esc = id.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`ggml-${esc}|${esc}[^\\\\/]*\\.bin`, "i");
}

/**
 * The catalog id of the model a whisper-cli command line really ran with: the `-m` file `<id>-<row id>.bin` (or
 * `ggml-<id>.bin`) -> `<id>`. Windows paths are read as such on any host. undefined when the line has no `-m <file>.bin`.
 */
export function modelIdFromCommand(commandLine: string): string | undefined {
  const file = /(?:^|\s)(?:-m|--model)(?:\s+|=)(?:"([^"]+\.bin)"|(\S+\.bin))(?=\s|$)/i.exec(commandLine);
  const full = file?.[1] ?? file?.[2];
  if (full === undefined) {
    return undefined;
  }
  const name = path.win32.basename(full).replace(/\.bin$/i, "");
  // the suffix is the row id of the installed model: 8+ letters and digits with at least one digit ("turbo", "q5_1" are not)
  const id = name.replace(/^ggml-/i, "").replace(/-(?=[a-z0-9]*\d)[a-z0-9]{8,}$/i, "");
  return id === "" ? undefined : id;
}

export interface SpeedInput {
  audioSeconds: number;
  wallSeconds: number;
  model: string;
  cpu: string;
}

/** «Расшифровка: 50 с звука за 12 с (×4,2 быстрее реального времени); модель base-q5_1; процессор …». For information. */
export function sttSpeedLine(s: SpeedInput): string {
  const ratio = s.wallSeconds > 0 ? s.audioSeconds / s.wallSeconds : Number.NaN;
  const times = Number.isFinite(ratio) ? ratio.toFixed(1).replace(".", ",") : "?";
  const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(1).replace(".", ","));
  return `${fmt(s.audioSeconds)} с звука расшифровано за ${fmt(s.wallSeconds)} с (×${times} от реального времени); модель ${s.model}; процессор ${s.cpu}`;
}

// ---- AC-06: no audio in what the Hub sent to the provider ----

/** Markers of an audio container, as text in a request body (JSON text has no raw binary). */
const containerMarkers = ["RIFF", "OggS", "ID3", "fLaC"] as const;
/** The same markers as the start of a base64 run: "RIFF" -> UklG, "OggS" -> T2dnUw, "ID3" -> SUQz, "fLaC" -> ZkxhQ. */
const base64Starts = ["UklG", "T2dnUw", "SUQz", "ZkxhQ"] as const;
/** An MPEG frame sync (0xFF 0xFB/0xFA/0xF3/0xF2...) is "//" at the start of base64 ("/+" and "//" after any alignment). */
const longBase64 = /[A-Za-z0-9+/]{4096,}={0,2}/;
const shortBase64 = /[A-Za-z0-9+/]{64,}={0,2}/g;

/** What is wrong with the body of one request, or [] when it carries no audio. */
export function audioFindings(body: unknown): string[] {
  const text = typeof body === "string" ? body : JSON.stringify(body);
  const found: string[] = [];
  for (const m of containerMarkers) {
    if (text.includes(m)) {
      found.push(`marker ${m}`);
    }
  }
  if (longBase64.test(text)) {
    found.push("a base64 run over 4 KiB");
  }
  for (const run of text.match(shortBase64) ?? []) {
    if (base64Starts.some((s) => run.startsWith(s)) || /^\/[/+]/.test(run)) {
      found.push(`a base64 run that starts like audio (${run.slice(0, 8)}…)`);
    }
  }
  if (/ÿ[ú-ûò-ó]/.test(text)) {
    found.push("an MPEG frame sync");
  }
  return found;
}

/**
 * The requests the fake could not read as a request: "body too large" is recorded WITHOUT a body and "malformed JSON" as raw
 * text, so the scan above cannot vouch for them. Each is a finding of its own, named by the request number.
 */
export function unscannedRequests(requests: readonly Pick<RecordedRequest, "seq" | "reason">[]): string[] {
  return requests.filter((r) => r.reason === "body too large" || r.reason === "malformed JSON").map((r) => `запрос ${r.seq}: ${r.reason}`);
}

/** The requests whose body has at least one of the words (case-insensitively, ё = е): the transcript really went to the Hub. */
export function requestsWithWords(requests: readonly Pick<RecordedRequest, "seq" | "body">[], words: readonly string[]): number[] {
  return requests.filter((r) => foundWords(typeof r.body === "string" ? r.body : JSON.stringify(r.body), words).length > 0).map((r) => r.seq);
}

/** Every finding of every request, named by the request number (seq) of the fake. */
export function audioInRequests(requests: readonly Pick<RecordedRequest, "seq" | "body">[]): string[] {
  return requests.flatMap((r) => audioFindings(r.body).map((f) => `запрос ${r.seq}: ${f}`));
}
