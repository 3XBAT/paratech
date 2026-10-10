#!/usr/bin/env bash
# Ralph loop (run from the repository root; code lives in $CODE_DIR): on every iteration a fresh agent session picks ONE pending task from tasks.json,
# implements and verifies it, records progress in progress.txt and commits.
# Memory between iterations lives only in files: tasks.json, progress.txt, git history.
#
# Env vars:
#   RALPH_AGENT=claude|codex   force the agent CLI (default: claude if present, else codex)
#   RALPH_MAX_ITERATIONS=50    hard cap on iterations
#   RALPH_MAX_STALLS=3         stop after N iterations in a row that did not reduce the pending count
#   RALPH_REVIEW=1|0           ask the `reviewer` subagent to check each commit (default 1)
set -uo pipefail

TASKS_FILE="${TASKS_FILE:-tasks.json}"
CODE_DIR="${CODE_DIR:-paratech}"   # directory with the code
MAX_ITERATIONS="${RALPH_MAX_ITERATIONS:-50}"
MAX_STALLS="${RALPH_MAX_STALLS:-3}"
USE_REVIEW="${RALPH_REVIEW:-1}"

if [[ ! -f "$TASKS_FILE" ]]; then
    echo "Файл $TASKS_FILE не найден. Сначала сгенерируйте его: docs/process/prd-to-tasks.prompt.md" >&2
    exit 1
fi

# Agent selection:
# - Set RALPH_AGENT=claude or RALPH_AGENT=codex to force.
# - Otherwise auto-detect (prefers Claude if available).
resolve_agent() {
    if [[ -n "${RALPH_AGENT:-}" ]]; then
        echo "$RALPH_AGENT"
        return 0
    fi
    if command -v claude >/dev/null 2>&1; then
        echo "claude"
        return 0
    fi
    if command -v codex >/dev/null 2>&1; then
        echo "codex"
        return 0
    fi
    return 1
}

run_agent() {
    local agent="$1"
    local prompt="$2"

    case "$agent" in
        claude)
            claude --permission-mode acceptEdits -p "$prompt"
            ;;
        codex)
            local output_file
            output_file="$(mktemp -t ralph_codex.XXXXXX)"
            # Use non-interactive Codex exec and capture only the last message.
            codex exec --full-auto --color never -C "$PWD" --output-last-message "$output_file" "$prompt" >/dev/null
            cat "$output_file"
            rm -f "$output_file"
            ;;
        *)
            echo "Unsupported agent: $agent" >&2
            return 1
            ;;
    esac
}

# Count tasks with a given status (pending|done|blocked). Tolerates any JSON spacing.
count_status() {
    grep -Ec "\"status\"[[:space:]]*:[[:space:]]*\"$1\"" "$TASKS_FILE" 2>/dev/null || true
}

# Never commit ralph work straight to main/master.
if ! git -C "$CODE_DIR" rev-parse --git-dir >/dev/null 2>&1; then
    echo "$CODE_DIR не находится в git-репозитории: ralph делает коммит кода после каждой задачи." >&2
    exit 1
fi
branch="$(git -C "$CODE_DIR" rev-parse --abbrev-ref HEAD)"
if [[ "$branch" == "main" || "$branch" == "master" ]]; then
    new_branch="ralph/$(date +%Y%m%d-%H%M)"
    echo "Текущая ветка кода: $branch. Создаю рабочую ветку $new_branch"
    git -C "$CODE_DIR" switch -c "$new_branch" || exit 1
fi

# tasks.json and progress.txt are committed by the script, not by the agent.
commit_workspace() {
    if git rev-parse --git-dir >/dev/null 2>&1; then
        git add tasks.json progress.txt >/dev/null 2>&1
        git commit -q -m "ralph: iteration $1 (tasks.json, progress.txt)" >/dev/null 2>&1 || true
    fi
}

read -r -d '' PROMPT_BASE <<'EOF'
@tasks.json @progress.txt @CLAUDE.md @docs/architecture.md
Ты работаешь в ralph-цикле: одна итерация = одна задача. Память между итерациями — только файлы и git.
1. Прочитай последние записи progress.txt и `git log --oneline -20`. Найди задачу со статусом pending и наивысшим приоритетом,
   у которой ВСЕ dependencies имеют статус done. Это должна быть задача, которую ТЫ считаешь наиболее приоритетной,
   не обязательно первая в списке. Работай ТОЛЬКО над ней.
2. Реализуй её в каталоге paratech/ строго по docs/architecture.md (слои, правила ARCH-NN) и жестким правилам из CLAUDE.md. Реализацию можно делегировать профильному субагенту
   (go-core — desktop-ядро, hub-dev — hub и shared, frontend — UI, tester — тесты), передав ему текст задачи и список файлов.
3. Проверь изменённые модули из каталога paratech/: `go vet ./<модуль>/...` и `go test -race ./<модуль>/...` (desktop, hub, shared; архитектурный тест входит в тесты),
   `GOOS=windows go vet ./desktop/...` для desktop, `npm run build` во frontend, если он затронут.
   Затем выполни ВСЕ test_steps задачи.
4. В tasks.json измени ТОЛЬКО поле status задачи: done, если все test_steps прошли; blocked, если автономно завершить нельзя
   (нужна проверка человеком на Windows, решение пользователя, внешний доступ) — причину опиши в progress.txt.
   Задачи удалять и переписывать ЗАПРЕЩЕНО.
5. Добавь запись в КОНЕЦ progress.txt в формате из шапки этого файла. Используй её как заметку для следующей итерации.
6. Сделай git commit кода ВНУТРИ paratech/ (`git -C paratech add -A . && git -C paratech commit`; сообщение: `TASK-XXX: краткое описание`). Не делай git push.
   Файлы tasks.json и progress.txt в корне репозитория сам не коммить: это сделает скрипт.
РАБОТАЙ ТОЛЬКО НАД ОДНОЙ ЗАДАЧЕЙ.
EOF

read -r -d '' PROMPT_REVIEW <<'EOF'
7. После коммита вызови субагента reviewer для проверки `git -C paratech show HEAD` (архитектурные нарушения ARCH-NN считаются блокерами). Замечания уровня blocker/high исправь отдельным коммитом
   и повтори проверки. Если исправить не удаётся — поставь status blocked и опиши проблему в progress.txt.
EOF

read -r -d '' PROMPT_END <<'EOF'
Если после твоей работы в tasks.json не осталось задач со статусом pending, выведи <promise>COMPLETE</promise>.
EOF

PROMPT="$PROMPT_BASE"
if [[ "$USE_REVIEW" == "1" ]]; then
    PROMPT="$PROMPT"$'\n'"$PROMPT_REVIEW"
fi
PROMPT="$PROMPT"$'\n'"$PROMPT_END"

iteration=1
stalls=0

while [ "$(count_status pending)" -gt 0 ]; do
    if [ "$iteration" -gt "$MAX_ITERATIONS" ]; then
        echo "Достигнут лимит итераций ($MAX_ITERATIONS). Остановка." >&2
        exit 2
    fi

    pending_before="$(count_status pending)"
    done_count="$(count_status done)"
    blocked="$(count_status blocked)"
    echo "Итерация $iteration"
    echo "-----------------------------------"
    echo "Задач pending: $pending_before, done: $done_count, blocked: $blocked"
    echo "-----------------------------------"

    agent=$(resolve_agent) || {
        echo "Не найден поддерживаемый агент. Установите 'claude' или 'codex', либо задайте RALPH_AGENT." >&2
        exit 1
    }

    result=$(run_agent "$agent" "$PROMPT") || echo "Агент завершился с ненулевым кодом." >&2
    echo "$result"

    commit_workspace "$iteration"
    pending_after="$(count_status pending)"
    if [ "$pending_after" -ge "$pending_before" ]; then
        stalls=$((stalls + 1))
        echo "Нет прогресса ($stalls из $MAX_STALLS)."
        if [ "$stalls" -ge "$MAX_STALLS" ]; then
            echo "Агент $MAX_STALLS итерации подряд не уменьшил число pending-задач. Остановка: проверьте progress.txt." >&2
            exit 3
        fi
    else
        stalls=0
    fi

    if [[ "$result" == *"<promise>COMPLETE</promise>"* ]] && [ "$(count_status pending)" -eq 0 ]; then
        echo "Все задачи обработаны. blocked: $(count_status blocked)"
        exit 0
    fi

    iteration=$((iteration + 1))
done

echo "Pending-задач не осталось. Итераций: $((iteration - 1)), blocked: $(count_status blocked)"
