// Renders report.md (Russian, quiet). Pure: the clock and the environment are inputs. Every returned text is scrubbed.
import type { ReportInput, Row, RunMode } from "./model.js";
import { envProblems, evaluate, firstLine, isEnvironmentMessage } from "./rows.js";
import { scrubSecrets } from "./scrub.js";

/** Budget of a whole run (ADR-0035 §6) and the guide for one pack. */
export const BudgetMs: Record<RunMode, number> = { SCRIPTED: 15 * 60_000, LIVE: 30 * 60_000 };
export const PackNoteMs = 5 * 60_000;

export const TableHeader = "| Функция | Режим | Итог | Шаг, где сломалось | Снимок |";

/** «3 мин 12 с», «42 с». */
export function formatDuration(ms: number): string {
  const total = Math.max(0, Math.round(ms / 1000));
  const min = Math.floor(total / 60);
  return min > 0 ? `${min} мин ${total % 60} с` : `${total} с`;
}

/** A markdown table cell: one line, `|` escaped. */
export function cell(text: string): string {
  const t = text.replace(/\s*[\r\n]+\s*/g, " ").trim();
  return t === "" ? "—" : t.replace(/\|/g, "\\|");
}

export function budgetWarning(mode: RunMode, elapsedMs: number): string | undefined {
  const limit = BudgetMs[mode];
  return elapsedMs > limit
    ? `Внимание: прогон занял ${formatDuration(elapsedMs)}, бюджет режима ${mode} — ${formatDuration(limit)}. Нужно ускорить прогон или разделить его.`
    : undefined;
}

export function packNotes(input: ReportInput): string[] {
  const perPack = new Map<string, number>();
  for (const t of input.tests) {
    if (t.pack) {
      perPack.set(t.pack, (perPack.get(t.pack) ?? 0) + t.durationMs);
    }
  }
  return [...perPack]
    .filter(([, ms]) => ms > PackNoteMs)
    .map(([pack, ms]) => `Примечание: пак «${pack}» занял ${formatDuration(ms)}, ориентир — ${formatDuration(PackNoteMs)}.`);
}

function link(label: string, target: string): string {
  return `[${label}](${target.replace(/ /g, "%20").replace(/\(/g, "%28").replace(/\)/g, "%29")})`;
}

function snapshotCell(row: Row): string {
  const shown = row.snapshots.slice(0, 6);
  if (shown.length === 0) {
    return "—";
  }
  return shown.map((s, i) => link(i === 0 ? "снимок" : `снимок ${i + 1}`, s)).join(" ");
}

function rowLine(r: Row): string {
  return `| ${cell(r.feature)} | ${cell(r.modes)} | ${cell(r.outcome)} | ${cell(r.step)} | ${snapshotCell(r)} |`;
}

/** The whole report.md text. */
export function renderReport(input: ReportInput, env: NodeJS.ProcessEnv = process.env): string {
  const { rows, counts } = evaluate(input);
  const problems = envProblems(input);
  const lines: string[] = ["# Отчёт E2E paratech", ""];

  lines.push(`Режим: ${input.mode}.${input.startedAt ? ` Начало прогона: ${input.startedAt}.` : ""}`, "");

  lines.push("## Функции", "");
  if (rows.length === 0) {
    lines.push("Тестов в прогоне нет.", "");
  } else {
    lines.push(TableHeader, "|---|---|---|---|---|", ...rows.map(rowLine), "");
    lines.push("«не проверено» не значит «сломано»: функция не проверялась, в скобках — что для этого нужно.", "");
  }

  if (problems.length > 0) {
    lines.push("## Проблемы среды", "", "Это проблемы машины или окружения, а не приложения: такие функции не проверены.", "");
    for (const p of problems) {
      lines.push(`- ${cell(p.kind)}: ${cell(p.message)}`);
    }
    lines.push("");
  }

  const runnerErrors = input.runnerErrors.filter((e) => !isEnvironmentMessage(e) && firstLine(e) !== "");
  if (runnerErrors.length > 0) {
    lines.push("## Ошибки раннера", "");
    for (const e of runnerErrors) {
      lines.push(`- ${cell(firstLine(e))}`);
    }
    lines.push("");
  }

  lines.push("## Итог прогона", "");
  lines.push(`- Режим: ${input.mode}`);
  lines.push(`- Время прогона: ${formatDuration(input.elapsedMs)}`);
  lines.push(
    `- Тесты: всего ${input.tests.length} — готово ${counts.ready}, сломано ${counts.broken}, не проверено ${counts.unchecked}`,
  );
  const warn = budgetWarning(input.mode, input.elapsedMs);
  if (warn) {
    lines.push(`- ${warn}`);
  }
  for (const n of packNotes(input)) {
    lines.push(`- ${n}`);
  }
  lines.push("");

  return scrubSecrets(lines.join("\n"), env);
}
