// Brings the screenshots attached to tests into <report>/screenshots/ and returns links relative to the report.
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";

import { SnapshotAttachment } from "../contract.js";
import { slugify } from "../steps/slug.js";

export const ScreenshotsDirName = "screenshots";

export interface AttachmentLike {
  name: string;
  contentType?: string;
  path?: string;
  body?: Uint8Array;
}

const sha1 = (data: Uint8Array): string => createHash("sha1").update(data).digest("hex");

export class ScreenshotStore {
  private readonly dir: string;
  private known?: Map<string, string>; // content hash -> file name
  private readonly seen = new Set<string>();

  constructor(reportDir: string) {
    this.dir = path.join(reportDir, ScreenshotsDirName);
  }

  /** Links ("screenshots/<file>.png") of the attachments named SnapshotAttachment; same content is linked once. */
  importFrom(attachments: readonly AttachmentLike[], hint: string): string[] {
    const links: string[] = [];
    for (const a of attachments) {
      if (a.name !== SnapshotAttachment) {
        continue;
      }
      const data = this.read(a);
      if (!data) {
        continue;
      }
      const link = `${ScreenshotsDirName}/${this.place(data, hint)}`;
      if (!links.includes(link)) {
        links.push(link);
      }
    }
    return links;
  }

  private read(a: AttachmentLike): Uint8Array | undefined {
    if (a.body) {
      return a.body;
    }
    if (a.path) {
      try {
        return readFileSync(a.path);
      } catch {
        return undefined; // the file is gone: no link rather than a broken one
      }
    }
    return undefined;
  }

  private place(data: Uint8Array, hint: string): string {
    mkdirSync(this.dir, { recursive: true });
    const known = this.index();
    const hash = sha1(data);
    const have = known.get(hash);
    if (have) {
      return have;
    }
    const base = slugify(hint, "snimok");
    let name = `${base}.png`;
    for (let n = 2; existsSync(path.join(this.dir, name)); n++) {
      name = `${base}-${n}.png`;
    }
    writeFileSync(path.join(this.dir, name), data);
    known.set(hash, name);
    this.seen.add(name);
    return name;
  }

  /** Hashes of the files in the folder; the step helper writes there too, so new files are picked up on every call. */
  private index(): Map<string, string> {
    this.known ??= new Map();
    for (const f of readdirSync(this.dir)) {
      if (f.endsWith(".png") && !this.seen.has(f)) {
        this.seen.add(f);
        this.known.set(sha1(readFileSync(path.join(this.dir, f))), f);
      }
    }
    return this.known;
  }
}
