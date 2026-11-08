// A reader of the Word file «Сохранить ▾» → «Word (.docx)» writes to <e2e-dir>/saved (TASK-381). A .docx is a zip: this reads the
// central directory and inflates the entries, which is all the pack needs to compare the text of the file with the draft.
// Not a spec. No dependency: node:zlib only.
import { inflateRawSync } from "node:zlib";

const EndOfCentralDirectory = 0x06054b50;
const CentralHeader = 0x02014b50;
const LocalHeader = 0x04034b50;
const Stored = 0;
const Deflated = 8;

/** The entries of a zip by name. Throws on anything that is not a plain zip (no zip64, no encryption). */
export function zipEntries(zip: Buffer): Map<string, Buffer> {
  let end = -1;
  for (let i = zip.length - 22; i >= Math.max(0, zip.length - 22 - 0xffff); i--) {
    if (zip.readUInt32LE(i) === EndOfCentralDirectory) {
      end = i;
      break;
    }
  }
  if (end < 0) {
    throw new Error("не zip-архив: нет записи конца центрального каталога");
  }
  const count = zip.readUInt16LE(end + 10);
  let at = zip.readUInt32LE(end + 16);
  const entries = new Map<string, Buffer>();
  for (let n = 0; n < count; n++) {
    if (zip.readUInt32LE(at) !== CentralHeader) {
      throw new Error(`zip: запись каталога ${n} повреждена`);
    }
    const method = zip.readUInt16LE(at + 10);
    const packed = zip.readUInt32LE(at + 20);
    const nameLength = zip.readUInt16LE(at + 28);
    const extraLength = zip.readUInt16LE(at + 30);
    const commentLength = zip.readUInt16LE(at + 32);
    const local = zip.readUInt32LE(at + 42);
    const name = zip.toString("utf8", at + 46, at + 46 + nameLength);
    if (zip.readUInt32LE(local) !== LocalHeader) {
      throw new Error(`zip: локальный заголовок «${name}» повреждён`);
    }
    const start = local + 30 + zip.readUInt16LE(local + 26) + zip.readUInt16LE(local + 28);
    const data = zip.subarray(start, start + packed);
    if (method === Stored) {
      entries.set(name, Buffer.from(data));
    } else if (method === Deflated) {
      entries.set(name, inflateRawSync(data));
    } else {
      throw new Error(`zip: «${name}» сжат способом ${method}, он не поддержан`);
    }
    at += 46 + nameLength + extraLength + commentLength;
  }
  return entries;
}

const entities: Readonly<Record<string, string>> = { "&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": '"', "&apos;": "'" };

/** The text of an XML part without tags: the entities decoded, the tags dropped (paragraph ends become a line break). */
export function xmlText(xml: string): string {
  return xml
    .replace(/<\/w:p>/g, "\n")
    .replace(/<[^>]+>/g, "")
    .replace(/&(?:amp|lt|gt|quot|apos);/g, (m) => entities[m] ?? m);
}

/** The text of word/document.xml of a .docx, one line per paragraph. */
export function docxText(docx: Buffer): string {
  const part = zipEntries(docx).get("word/document.xml");
  if (!part) {
    throw new Error("в файле нет word/document.xml: это не документ Word");
  }
  return xmlText(part.toString("utf8"));
}
