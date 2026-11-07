#!/usr/bin/env bash
# check-glibc.sh — fail if any ELF file under DIR needs a newer glibc than MAX (default 2.35, Ubuntu 22.04).
#
# An AppImage runs against the host's libc, so a binary linked on a newer system fails to start on
# older ones ("version `GLIBC_2.38' not found"). The AppImage catalog rejects such builds. Also flags
# GLIBC_ABI_DT_RELR, which toolchains newer than glibc 2.36 emit and older loaders do not know.
#
# Usage: scripts/check-glibc.sh DIR [MAX]
set -euo pipefail

DIR="${1:?usage: check-glibc.sh DIR [MAX]}"
MAX="${2:-2.35}"
command -v objdump >/dev/null || { echo "error: objdump (binutils) is required" >&2; exit 2; }

newer() { [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1)" != "$2" ]; }  # true when $1 > $2

bad=0 checked=0
while IFS= read -r -d '' f; do
    [ "$(head -c4 "$f" 2>/dev/null | od -An -c | tr -d ' ')" = '177ELF' ] || continue
    checked=$((checked + 1))
    syms="$(objdump -T "$f" 2>/dev/null | grep -oE 'GLIBC_[0-9][0-9.]*|GLIBC_ABI_[A-Z_]+' | sort -uV || true)"
    [ -n "$syms" ] || continue
    worst=""
    while IFS= read -r s; do
        case "$s" in
            GLIBC_ABI_DT_RELR) worst="$s"; break ;;
            GLIBC_[0-9]*) v="${s#GLIBC_}"; if newer "$v" "${worst#GLIBC_}" 2>/dev/null || [ -z "$worst" ]; then worst="$s"; fi ;;
        esac
    done <<<"$syms"
    case "$worst" in
        GLIBC_ABI_DT_RELR) echo "FAIL ${f#"$DIR"/}: needs GLIBC_ABI_DT_RELR (built with a too-new linker)"; bad=$((bad + 1)) ;;
        GLIBC_[0-9]*) if newer "${worst#GLIBC_}" "$MAX"; then echo "FAIL ${f#"$DIR"/}: needs $worst (> GLIBC_$MAX)"; bad=$((bad + 1)); fi ;;
    esac
done < <(find "$DIR" -type f -print0)

echo "checked $checked ELF files against glibc $MAX: $bad too new"
[ "$bad" -eq 0 ]
