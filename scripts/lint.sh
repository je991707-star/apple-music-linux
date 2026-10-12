#!/bin/sh
# Run every style and static check the project enforces (CI runs the same set).
#   scripts/lint.sh        check
#   scripts/lint.sh --fix  apply gofmt / clang-format / eslint fixes first
set -eu
repo=$(cd "$(dirname "$0")/.." && pwd)
fix=0; [ "${1:-}" = "--fix" ] && fix=1
fail=0
step() { printf '\n== %s\n' "$1"; }
run() { "$@" || fail=1; }

step "Go: gofmt -s"
cd "$repo/engine"
[ "$fix" = 1 ] && gofmt -s -w .
out=$(gofmt -s -l .); [ -z "$out" ] || { echo "$out"; fail=1; }

step "Go: vet"
run go vet ./...

step "Go: staticcheck"
if command -v staticcheck >/dev/null 2>&1; then run staticcheck ./...
elif [ -x "$(go env GOPATH)/bin/staticcheck" ]; then run "$(go env GOPATH)/bin/staticcheck" ./...
else echo "staticcheck not installed: go install honnef.co/go/tools/cmd/staticcheck@2026.2.2"; fail=1; fi

step "C: clang-format (drm/, own sources only)"
if command -v clang-format >/dev/null 2>&1; then
    if [ "$fix" = 1 ]; then make -s -C "$repo/drm" format; fi
    run make -s -C "$repo/drm" format-check
else echo "clang-format not installed (pip install clang-format==23.1.1)"; fail=1; fi

step "JS: eslint"
cd "$repo/electron"
if [ -x node_modules/.bin/eslint ]; then
    if [ "$fix" = 1 ]; then run npx eslint --fix .; else run npx eslint .; fi
else echo "eslint missing: run npm ci in electron/"; fail=1; fi

step "Shell: syntax"
for f in "$repo"/scripts/*.sh "$repo"/electron/build.sh; do
    [ -f "$f" ] || continue
    case "$(head -1 "$f")" in *bash*) run bash -n "$f" ;; *) run sh -n "$f" ;; esac
done

[ "$fail" = 0 ] && printf '\nAll checks passed.\n' || { printf '\nSome checks failed.\n'; exit 1; }
