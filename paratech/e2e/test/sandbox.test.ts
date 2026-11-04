import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, test } from "node:test";
import { pathToFileURL } from "node:url";

import { parseCliArgs, playwrightEnv } from "../src/cli.js";
import { EnvMode } from "../src/contract.js";
import { isEnvironmentError } from "../src/hub/index.js";
import { assertWipeTarget, prepareControlDir, RunRootName, sandboxPaths, type SandboxPaths, wipeRunRoot } from "../src/sandbox/paths.js";
import { EnvNoBuild, EnvWindow, readRunOptions } from "../src/sandbox/options.js";

/** A fake repo in a temp dir: <tmp>/paratech/{dist,e2e/src/contract.ts,e2e/package.json}. */
function fakeRepo(): { root: string; paths: SandboxPaths; cleanup(): void } {
  const root = mkdtempSync(path.join(os.tmpdir(), "e2e-sandbox-"));
  const e2e = path.join(root, "paratech", "e2e");
  mkdirSync(path.join(e2e, "src"), { recursive: true });
  mkdirSync(path.join(root, "paratech", "dist"), { recursive: true });
  writeFileSync(path.join(e2e, "package.json"), "{}");
  writeFileSync(path.join(e2e, "src", "contract.ts"), "");
  const paths = sandboxPaths(pathToFileURL(path.join(e2e, "src", "sandbox", "paths.ts")).href);
  return { root, paths, cleanup: () => rmSync(root, { recursive: true, force: true }) };
}

describe("sandbox paths", () => {
  test("are found from the module location, not from the cwd", () => {
    const { root, paths, cleanup } = fakeRepo();
    try {
      assert.equal(paths.e2eDir, path.join(root, "paratech", "e2e"));
      assert.equal(paths.runRoot, path.join(root, "paratech", "dist", RunRootName));
      assert.equal(paths.ctlDir, path.join(paths.runRoot, "ctl"));
      assert.equal(paths.dataDir, path.join(paths.ctlDir, "data"));
      assert.equal(paths.fixturesDir, path.join(paths.ctlDir, "fixtures"));
      assert.equal(paths.exePath, path.join(root, "paratech", "dist", "e2e", "paratech.exe"));
    } finally {
      cleanup();
    }
  });

  test("the real repo layout resolves from both src/ and build/src/", () => {
    const real = sandboxPaths();
    assert.ok(real.runRoot.endsWith(path.join("dist", RunRootName)));
    assert.ok(existsSync(path.join(real.e2eDir, "src", "contract.ts")));
    assert.ok(existsSync(real.buildScript), real.buildScript);
  });
});

describe("the wipe guard", () => {
  const repo = path.resolve(os.tmpdir(), "somewhere", "paratech");
  const good = path.join(repo, "dist", RunRootName);

  test("accepts exactly <paratech>/dist/e2e-run", () => {
    assertWipeTarget(good, repo);
    assertWipeTarget(path.join(repo, "dist", "x", "..", RunRootName), repo); // normalised first
  });

  test("refuses everything else", () => {
    const refused = [
      path.join(repo, "dist"),
      path.join(repo, "dist", "e2e"),
      path.join(repo, "dist", RunRootName, "ctl"),
      path.join(repo, RunRootName), // wrong parent
      path.join(repo, "dist", "sub", RunRootName), // too deep
      path.join(os.tmpdir(), "dist", RunRootName), // another repo
      path.parse(repo).root,
      repo,
      path.join(repo, "dist", RunRootName + "-old"),
      "",
      ".",
    ];
    for (const target of refused) {
      assert.throws(() => assertWipeTarget(target, repo), (e: unknown) => isEnvironmentError(e), JSON.stringify(target));
    }
  });

  test("refuses a symbolic link in place of the run root", (t) => {
    const { root, paths, cleanup } = fakeRepo();
    try {
      const elsewhere = path.join(root, "elsewhere");
      mkdirSync(elsewhere);
      try {
        symlinkSync(elsewhere, paths.runRoot, "junction");
      } catch {
        t.skip("cannot create a link here");
        return;
      }
      assert.throws(() => assertWipeTarget(paths.runRoot, paths.paratechDir), /ссылка/);
    } finally {
      cleanup();
    }
  });
});

describe("wipe and prepare", () => {
  test("wipes old contents and leaves an empty run root", () => {
    const { paths, cleanup } = fakeRepo();
    try {
      mkdirSync(path.join(paths.runRoot, "ctl", "data"), { recursive: true });
      writeFileSync(path.join(paths.runRoot, "ctl", "data", "old.db"), "old");
      writeFileSync(path.join(paths.runRoot, "state.json"), "{}");
      wipeRunRoot(paths);
      assert.ok(statSync(paths.runRoot).isDirectory());
      assert.deepEqual(readdirSync(paths.runRoot), []);
    } finally {
      cleanup();
    }
  });

  test("a missing run root is created; its neighbours in dist/ stay", () => {
    const { paths, cleanup } = fakeRepo();
    try {
      mkdirSync(path.join(paths.distDir, "e2e"));
      writeFileSync(path.join(paths.distDir, "e2e", "paratech.exe"), "exe");
      wipeRunRoot(paths);
      assert.ok(existsSync(paths.runRoot));
      assert.equal(readFileSync(path.join(paths.distDir, "e2e", "paratech.exe"), "utf8"), "exe");
    } finally {
      cleanup();
    }
  });

  test("a wrong run root is refused before anything is removed", () => {
    const { paths, cleanup } = fakeRepo();
    try {
      const victim = path.join(paths.distDir, "important");
      mkdirSync(victim);
      writeFileSync(path.join(victim, "keep.txt"), "keep");
      assert.throws(() => wipeRunRoot({ ...paths, runRoot: victim }), (e: unknown) => isEnvironmentError(e));
      assert.equal(readFileSync(path.join(victim, "keep.txt"), "utf8"), "keep");
    } finally {
      cleanup();
    }
  });

  test("without a fixtures folder the control dir gets an empty plain one", () => {
    const { paths, cleanup } = fakeRepo();
    try {
      wipeRunRoot(paths);
      prepareControlDir(paths);
      assert.ok(statSync(paths.dataDir).isDirectory());
      assert.ok(statSync(paths.logsDir).isDirectory());
      assert.ok(statSync(paths.fixturesDir).isDirectory());
      assert.deepEqual(readdirSync(paths.fixturesDir), []);
    } finally {
      cleanup();
    }
  });

  test("fixtures are copied into <ctl>/fixtures", () => {
    const { paths, cleanup } = fakeRepo();
    try {
      mkdirSync(path.join(paths.fixturesSource, "audio"), { recursive: true });
      writeFileSync(path.join(paths.fixturesSource, "schedule.ics"), "BEGIN:VCALENDAR");
      writeFileSync(path.join(paths.fixturesSource, "audio", "clip.wav"), "RIFF");
      wipeRunRoot(paths);
      prepareControlDir(paths);
      assert.equal(readFileSync(path.join(paths.fixturesDir, "schedule.ics"), "utf8"), "BEGIN:VCALENDAR");
      assert.equal(readFileSync(path.join(paths.fixturesDir, "audio", "clip.wav"), "utf8"), "RIFF");
      assert.ok(readFileSync(path.join(paths.fixturesSource, "schedule.ics")), "the source stays");
    } finally {
      cleanup();
    }
  });
});

describe("run options", () => {
  test("defaults: scripted, build, default window", () => {
    assert.deepEqual(readRunOptions({}), { mode: "scripted", noBuild: false });
  });

  test("live, no-build and window come from the environment of the wrapper", () => {
    assert.deepEqual(readRunOptions({ [EnvMode]: "live", [EnvNoBuild]: "1", [EnvWindow]: "1280x800" }), {
      mode: "live",
      noBuild: true,
      window: "1280x800",
    });
  });

  test("an unknown mode or a malformed window is refused", () => {
    assert.throws(() => readRunOptions({ [EnvMode]: "prod" }), /scripted или live/);
    assert.throws(() => readRunOptions({ [EnvWindow]: "wide" }), /1280x800/);
  });
});

describe("cli wrapper", () => {
  test("--no-build and --live are taken, everything else goes to playwright in order", () => {
    assert.deepEqual(parseCliArgs(["--grep", "@smoke", "--no-build", "specs/smoke", "--live"]), {
      noBuild: true,
      live: true,
      rest: ["--grep", "@smoke", "specs/smoke"],
    });
    assert.deepEqual(parseCliArgs([]), { noBuild: false, live: false, rest: [] });
    assert.deepEqual(parseCliArgs(["--grep", "@agent"]).rest, ["--grep", "@agent"]);
  });

  test("the flags become the environment of the Playwright process", () => {
    const base = { PATH: "p", [EnvMode]: "live" }; // a stale value must not win over the flags
    assert.deepEqual(playwrightEnv(base, parseCliArgs([])), { PATH: "p", [EnvMode]: "scripted", [EnvNoBuild]: "0" });
    assert.deepEqual(playwrightEnv(base, parseCliArgs(["--live", "--no-build"])), { PATH: "p", [EnvMode]: "live", [EnvNoBuild]: "1" });
  });
});
