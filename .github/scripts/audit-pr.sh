#!/usr/bin/env bash
# Independent audit of a loop PR (docs/process/supervision.md). Usage: audit-pr.sh <pr-number>
# Prints FLAG lines for things the supervisor must look at; exit code 1 if any flag was raised.
set -u
PR="${1:?usage: audit-pr.sh <pr-number>}"
REPO="${REPO:-3XBAT/paratech}"
GH="${GH:-gh}"
cd "$(git rev-parse --show-toplevel)" || exit 1

HEAD_REF=$("$GH" pr view "$PR" --repo "$REPO" --json headRefName --jq .headRefName)
git fetch -q origin "$HEAD_REF" master 2>/dev/null
RANGE="origin/master...origin/$HEAD_REF"
flags=0
flag() { echo "FLAG: $*"; flags=$((flags + 1)); }

echo "PR #$PR branch $HEAD_REF"
echo "changed files:"; git diff --stat "$RANGE" | tail -1

# tasks.json: only status lines may change
extra=$(git diff -U0 "$RANGE" -- tasks.json | grep -E '^[+-]' | grep -vE '^(\+\+\+|---)' | grep -v '"status"')
[ -n "$extra" ] && flag "tasks.json changed beyond status lines"

# progress.txt: append-only
removed=$(git diff -U0 "$RANGE" -- progress.txt | grep -E '^-' | grep -vE '^---')
[ -n "$removed" ] && flag "progress.txt has removed lines"

# sensitive paths
sens=$(git diff --name-only "$RANGE" | grep -E '^(\.claude/|\.github/|CLAUDE\.md|ralph\.sh|docs/architecture\.md|docs/prd/|docs/process/|paratech/\.golangci\.yml|paratech/go\.work)' || true)
[ -n "$sens" ] && flag "sensitive paths touched: $(echo "$sens" | tr '\n' ' ')"

# unexpected locations
other=$(git diff --name-only "$RANGE" | grep -vE '^(paratech/|docs/|tasks\.json|progress\.txt|\.github/|\.claude/|CLAUDE\.md|ralph\.sh)' || true)
[ -n "$other" ] && flag "files in unexpected locations: $(echo "$other" | tr '\n' ' ')"

# weakened verification
added=$(git diff -U0 "$RANGE" -- '*.go' | grep -E '^\+' | grep -vE '^\+\+\+')
echo "$added" | grep -qE 't\.Skip(f|Now)?\(' && flag "added t.Skip (check it is conditional on the environment)"
echo "$added" | grep -qE 'nolint' && flag "added nolint directives (each needs a reason)"
echo "$added" | grep -qE '//go:build ignore' && flag "added //go:build ignore"
del_tests=$(git diff --name-only --diff-filter=D "$RANGE" | grep -E '_test\.go$' || true)
[ -n "$del_tests" ] && flag "deleted test files: $del_tests"

# dependencies
newdeps=$(git diff -U0 "$RANGE" -- 'paratech/*/go.mod' | grep -E '^\+\s+[a-z0-9.\-]+\.[a-z]+/' | grep -vE '// indirect' || true)
[ -n "$newdeps" ] && flag "go.mod direct requirements changed: $(echo "$newdeps" | tr '\n' ';')"

# secrets
if echo "$added" | grep -qE 'BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY|ghp_[A-Za-z0-9]{20,}|sk-ant-[A-Za-z0-9_-]{10,}|AKIA[0-9A-Z]{16}'; then
  flag "secret-looking string added"
fi

# CI
echo "CI:"; "$GH" pr checks "$PR" --repo "$REPO" 2>&1 | awk '{print "  " $1 ": " $2}'
"$GH" pr checks "$PR" --repo "$REPO" 2>&1 | grep -qE 'fail|pending' && flag "CI is not green"

echo "flags: $flags"
[ "$flags" -eq 0 ]
