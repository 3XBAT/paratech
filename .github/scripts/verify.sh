#!/usr/bin/env bash
# Rebase the current branch on origin/master and run every check of a task. Run from anywhere in the repo.
# Needs: Go 1.26.9+ (auto toolchain), golangci-lint v2 first on PATH, python, sqlc via `go run` (network on first use).
# Generated sqlcgen conflicts during the rebase are resolved with our side and regenerated afterwards.
set -u
cd "$(git rev-parse --show-toplevel)" || exit 1
export GIT_TERMINAL_PROMPT=0 GIT_EDITOR=true

git fetch -q origin
git stash -q -u 2>/dev/null
git rebase origin/master >/dev/null 2>&1
for _ in $(seq 12); do
  U=$(git diff --name-only --diff-filter=U)
  [ -z "$U" ] && break
  for f in $U; do git checkout --theirs "$f" >/dev/null 2>&1 && git add "$f"; done
  git rebase --continue >/dev/null 2>&1
done
git stash pop -q 2>/dev/null
if [ -d .git/rebase-merge ] || [ -d .git/rebase-apply ]; then echo "REBASE_UNRESOLVED"; git status --short | head; exit 2; fi

cd paratech || exit 1
(cd desktop/internal/adapters/sqlite && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate >/dev/null 2>&1; git status --porcelain -- sqlcgen | sed 's/^/SQLC_CHANGED /')
echo "gofmt: $(gofmt -l desktop hub shared | tr '\n' ' ')"
CGO_ENABLED=0 go build ./desktop/... ./hub/... ./shared/... || { echo BUILD_FAIL; exit 3; }
go vet ./desktop/... ./hub/... ./shared/... || { echo VET_FAIL; exit 3; }
GOOS=windows go vet ./desktop/... || { echo WINVET_FAIL; exit 3; }
PARATECH_REQUIRE_DOCS=1 go test -race -count=1 ./desktop/... ./hub/... ./shared/... 2>&1 | grep -v "no test files" | grep -v "^ok"
[ "${PIPESTATUS[0]}" = 0 ] || { echo TEST_FAIL; exit 3; }
golangci-lint run ./desktop/... ./hub/... ./shared/... || { echo LINT_FAIL; exit 3; }
GOOS=windows golangci-lint run ./desktop/... || { echo WINLINT_FAIL; exit 3; }
python ../.github/scripts/vulncheck.py || { echo VULN_FAIL; exit 3; }
echo ALL_GREEN
git -C .. status --short | head -5
git -C .. log --oneline origin/master..HEAD | head -12
