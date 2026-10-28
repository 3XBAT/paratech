// Subjects from the schedule (TASK-311): the binding calls, the codes pinned to library_schedule.go and the strict parsing.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  createFromSchedule,
  methods,
  parseCreateFromSchedule,
  parseScheduleSubjects,
  parseUndoFromSchedule,
  scheduleCreateProblems,
  scheduleGroupActions,
  scheduleKeptReasons,
  scheduleKindWords,
  scheduleKinds,
  scheduleLinks,
  scheduleListProblems,
  scheduleSkipReasons,
  scheduleSubjects,
  scheduleUndoProblems,
  undoFromSchedule,
} from "../.test-build/bindings/library.js";

const prefix = "paratech/desktop/internal/input/wails.LibraryService.";

function kind(k = "lecture", lessons = 4) {
  return { kind: k, lessons };
}

function member(over = {}) {
  return {
    key: "m1", name: "Мат. анализ", spellings: ["Мат. анализ", "МатАн"], kinds: [kind("seminar", 2)], lessons: 2, recordings: 1,
    doubtful: true, link: "abbreviation", ...over,
  };
}

function group(over = {}) {
  return {
    key: "g1", name: "Математический анализ", aliases: ["Матан"], kinds: [kind("lecture", 4), kind("seminar", 2)], lessons: 6,
    recordings: 3, doubtful: false, members: [], action: "create", subjectId: "", subjectName: "", ...over,
  };
}

function proposal(over = {}) {
  return { token: "tok-1", groups: [group()], omitted: 0, problem: "", ...over };
}

function created(over = {}) {
  return {
    created: [{ id: "s1", key: "g1", name: "Физика" }], aliased: [{ subjectId: "s2", aliases: ["Матан"] }],
    skipped: [{ key: "g3", subjectId: "", reason: "name_taken" }], problem: "", ...over,
  };
}

function undone(over = {}) {
  return {
    removed: ["s1"], kept: [{ id: "s3", name: "Химия", reason: "has_files" }], unaliased: [{ subjectId: "s2", aliases: ["Матан"] }],
    keptAliases: [{ subjectId: "s4", aliases: ["Инф"], reason: "stale" }], problem: "", ...over,
  };
}

test("method names, codes and kinds match library_schedule.go", () => {
  const go = readFileSync(new URL("../../internal/input/wails/library_schedule.go", import.meta.url), "utf8");
  const value = (name) => go.match(new RegExp(`\\b${name}\\s*=\\s*"([^"]*)"`))[1];
  const goMethods = new Set([...go.matchAll(/^func \(s \*LibraryService\) ([A-Z]\w*)\(/gm)].map((m) => m[1]));
  for (const m of [methods.scheduleSubjects, methods.createFromSchedule, methods.undoFromSchedule]) {
    assert.ok(goMethods.has(m.slice(prefix.length)), m);
  }

  const problem = (n) => value(`ScheduleProblem${n}`);
  const stale = problem("Stale"), invalid = problem("Invalid"), taken = problem("NameTaken"), busy = problem("Busy"), unavailable = problem("Unavailable");
  assert.deepEqual([...scheduleCreateProblems].sort(), ["", stale, invalid, taken, busy, unavailable].sort());
  assert.deepEqual([...scheduleUndoProblems].sort(), ["", invalid, busy, unavailable].sort());
  assert.deepEqual([...scheduleListProblems].sort(), ["", unavailable].sort());

  assert.deepEqual(
    [...scheduleKinds],
    [value("ScheduleKindLecture"), value("ScheduleKindSeminar"), value("ScheduleKindLab"), value("ScheduleKindUnknown")],
  );
  assert.deepEqual(
    [...scheduleLinks],
    [value("ScheduleLinkSame"), value("ScheduleLinkAbbreviation"), value("ScheduleLinkTypo")],
  );
  assert.deepEqual([...scheduleGroupActions], [value("ScheduleActionCreate"), value("ScheduleActionAliasOf")]);
  assert.deepEqual([...scheduleSkipReasons].sort(), ["name_taken", "too_many_aliases", "subject_gone"].sort());
  assert.deepEqual([...scheduleKeptReasons].sort(), ["has_files", "has_notes"].sort());
  for (const reason of [...scheduleSkipReasons, ...scheduleKeptReasons, "stale"]) {
    assert.ok(go.includes(`"${reason}"`), reason);
  }
});

test("kind words", () => {
  assert.deepEqual(
    scheduleKinds.map((k) => scheduleKindWords[k]),
    ["лекции", "семинары", "лабораторные", "пары"],
  );
});

test("scheduleSubjects calls the binding with no arguments and parses the proposal", async () => {
  const calls = [];
  const reply = proposal({
    groups: [
      group(),
      group({ key: "g2", name: "Физика", doubtful: true, members: [member(), member({ key: "m2", doubtful: false, link: "same" })] }),
      group({ key: "g3", name: "Химия", action: "alias_of", subjectId: "s9", subjectName: "Химия" }),
    ],
    omitted: 2,
  });
  const out = await scheduleSubjects(async (method, ...args) => {
    calls.push([method, ...args]);
    return reply;
  });
  assert.deepEqual(calls, [[prefix + "ScheduleSubjects"]]);
  assert.deepEqual(out, reply);
  assert.equal(out.groups[1].members[0].link, "abbreviation");
  assert.equal(out.groups[2].action, "alias_of");
});

test("an empty proposal and the unavailable problem are valid", async () => {
  const empty = proposal({ token: "", groups: [] });
  assert.deepEqual(await scheduleSubjects(async () => empty), empty);
  const down = proposal({ token: "", groups: [], problem: "unavailable" });
  assert.deepEqual(await scheduleSubjects(async () => down), down);
});

test("createFromSchedule passes the token, then the choices exactly as given", async () => {
  const calls = [];
  const choices = [
    { group: "g1", action: "create", name: "Физика", members: [{ key: "m1", action: "separate", name: "Оптика" }, { key: "m2", action: "keep", name: "" }] },
    { group: "g2", action: "alias_of", name: "", members: [] },
    { group: "g3", action: "skip", name: "", members: [{ key: "m3", action: "skip", name: "" }] },
  ];
  const before = JSON.stringify(choices);
  const out = await createFromSchedule(
    async (method, ...args) => {
      calls.push([method, ...args]);
      return created();
    },
    "tok-1",
    choices,
  );
  assert.equal(calls.length, 1);
  assert.equal(calls[0][0], prefix + "CreateFromSchedule");
  assert.equal(calls[0].length, 3);
  assert.equal(calls[0][1], "tok-1");
  assert.deepEqual(calls[0][2], choices);
  assert.equal(JSON.stringify(calls[0][2]), before);
  assert.deepEqual(out, created());
});

test("every create problem is accepted and keeps the lists", async () => {
  for (const problem of scheduleCreateProblems) {
    const out = await createFromSchedule(async () => created({ problem }), "t", []);
    assert.equal(out.problem, problem);
    assert.equal(out.created.length, 1);
  }
  const none = created({ created: [], aliased: [], skipped: [], problem: "busy" });
  assert.deepEqual(parseCreateFromSchedule(none), none);
  for (const reason of scheduleSkipReasons) {
    assert.equal(parseCreateFromSchedule(created({ skipped: [{ key: "k", subjectId: "", reason }] })).skipped[0].reason, reason);
  }
});

test("undoFromSchedule sends only the token", async () => {
  const calls = [];
  const out = await undoFromSchedule(async (method, ...args) => {
    calls.push([method, ...args]);
    return undone();
  }, "tok-1");
  assert.deepEqual(calls, [[prefix + "UndoFromSchedule", "tok-1"]]);
  assert.deepEqual(out, undone());
});

test("every undo problem is accepted and keeps the lists", () => {
  for (const problem of scheduleUndoProblems) {
    assert.equal(parseUndoFromSchedule(undone({ problem })).problem, problem);
  }
  for (const reason of scheduleKeptReasons) {
    assert.equal(parseUndoFromSchedule(undone({ kept: [{ id: "s", name: "n", reason }] })).kept[0].reason, reason);
  }
  const none = undone({ removed: [], kept: [], unaliased: [], keptAliases: [] });
  assert.deepEqual(parseUndoFromSchedule(none), none);
});

test("a malformed proposal is a parse error with a fixed text", () => {
  const bad = [
    null,
    [],
    "x",
    proposal({ problem: "stale" }),
    proposal({ problem: "oops" }),
    proposal({ problem: null }),
    proposal({ groups: null }),
    proposal({ groups: {} }),
    proposal({ omitted: -1 }),
    proposal({ omitted: "1" }),
    proposal({ token: 5 }),
    proposal({ groups: [group({ action: "skip" })] }),
    proposal({ groups: [group({ action: "other" })] }),
    proposal({ groups: [group({ kinds: [kind("exam")] })] }),
    proposal({ groups: [group({ kinds: null })] }),
    proposal({ groups: [group({ kinds: [kind("lecture", 1.5)] })] }),
    proposal({ groups: [group({ aliases: null })] }),
    proposal({ groups: [group({ aliases: [1] })] }),
    proposal({ groups: [group({ members: null })] }),
    proposal({ groups: [group({ doubtful: "yes" })] }),
    proposal({ groups: [group({ lessons: "6" })] }),
    proposal({ groups: [group({ name: undefined })] }),
    proposal({ groups: [group({ members: [member({ link: "synonym" })] })] }),
    proposal({ groups: [group({ members: [member({ spellings: null })] })] }),
    proposal({ groups: [group({ members: [member({ kinds: [kind("unknown_kind")] })] })] }),
    proposal({ groups: [group({ members: [member({ recordings: null })] })] }),
    proposal({ groups: [null] }),
  ];
  for (const raw of bad) {
    assert.throws(() => parseScheduleSubjects(raw), (err) => err instanceof Error);
  }
  const missing = proposal();
  delete missing.omitted;
  assert.throws(() => parseScheduleSubjects(missing));
  const noSubject = group();
  delete noSubject.subjectName;
  assert.throws(() => parseScheduleSubjects(proposal({ groups: [noSubject] })));
});

test("a malformed create or undo reply is a parse error", async () => {
  const badCreate = [
    null,
    created({ problem: "failed" }),
    created({ problem: "" + undefined }),
    created({ created: null }),
    created({ aliased: null }),
    created({ skipped: null }),
    created({ skipped: [{ key: "k", subjectId: "", reason: "because" }] }),
    created({ skipped: [{ key: "k", subjectId: "", reason: "has_files" }] }),
    created({ created: [{ id: "s", key: "k" }] }),
    created({ aliased: [{ subjectId: "s", aliases: null }] }),
  ];
  for (const raw of badCreate) {
    await assert.rejects(createFromSchedule(async () => raw, "t", []), (err) => err instanceof Error);
  }
  const noProblem = created();
  delete noProblem.problem;
  assert.throws(() => parseCreateFromSchedule(noProblem));

  const badUndo = [
    null,
    undone({ problem: "stale" }),
    undone({ problem: "name_taken" }),
    undone({ problem: "x" }),
    undone({ removed: null }),
    undone({ removed: [1] }),
    undone({ kept: null }),
    undone({ kept: [{ id: "s", name: "n", reason: "stale" }] }),
    undone({ kept: [{ id: "s", name: "n", reason: "unknown" }] }),
    undone({ unaliased: null }),
    undone({ keptAliases: null }),
    undone({ keptAliases: [{ subjectId: "s", aliases: ["a"], reason: "has_files" }] }),
    undone({ keptAliases: [{ subjectId: "s", aliases: ["a"] }] }),
  ];
  for (const raw of badUndo) {
    await assert.rejects(undoFromSchedule(async () => raw, "t"), (err) => err instanceof Error);
  }
});

test("parse errors never carry the text of the reply", () => {
  const secret = "СЕКРЕТ-из-расписания";
  const raws = [
    proposal({ groups: [group({ name: secret, action: secret })] }),
    proposal({ groups: [group({ name: secret, kinds: [kind(secret)] })] }),
    proposal({ problem: secret }),
  ];
  for (const raw of raws) {
    try {
      parseScheduleSubjects(raw);
      assert.fail("must throw");
    } catch (err) {
      assert.ok(err instanceof Error);
      assert.ok(!err.message.includes(secret), err.message);
    }
  }
  assert.throws(() => parseCreateFromSchedule(created({ skipped: [{ key: secret, subjectId: "", reason: secret }] })), (err) => !err.message.includes(secret));
});

test("a rejected call is passed on, not turned into a reply", async () => {
  await assert.rejects(scheduleSubjects(async () => Promise.reject(new Error("boom"))));
  await assert.rejects(createFromSchedule(async () => Promise.reject(new Error("boom")), "t", []));
  await assert.rejects(undoFromSchedule(async () => Promise.reject(new Error("boom")), "t"));
});
