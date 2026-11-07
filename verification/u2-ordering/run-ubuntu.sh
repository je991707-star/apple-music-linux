#!/usr/bin/env bash
set -u
cd /var/home/h97m/Projects/adam-u2-ordering
source verification/u2-ordering/env.sh
export GOCACHE=$U2_LAB/go-cache-ubuntu TMPDIR=$U2_LAB/tmp-ubuntu
mkdir -p "$GOCACHE" "$TMPDIR"
# Native tests compile directly from the production Engine module; no copied Player.
cd engine
"$U2_GO" test -race -tags vlc_ordering_test -v -count=1 -timeout 60s ./core/vlc -run '^TestNative' > "$U2_LAB/native-ubuntu.log" 2>&1
printf '%s\n' "$?" > "$U2_LAB/native-ubuntu.exit"
"$U2_GO" test -race -v -count=1 -timeout 60s ./internal/playbackcmd ./core/diskcache > "$U2_LAB/gate-diskcache-ubuntu.log" 2>&1
printf '%s\n' "$?" > "$U2_LAB/gate-diskcache-ubuntu.exit"
"$U2_GO" test -race -v -count=1 -timeout 60s ./cmd -run 'Test(VLC|ALAC|InvalidateALAC)' > "$U2_LAB/cmd-ubuntu.log" 2>&1
printf '%s\n' "$?" > "$U2_LAB/cmd-ubuntu.exit"
PYTHONDONTWRITEBYTECODE=1 python3 /var/home/h97m/Projects/adam-vlc-fix/scripts/vlc-runtime.py --max-glibc 2.35 "$U2_RUNTIME" "$U2_RUNTIME" > "$U2_LAB/validator-ubuntu.log" 2>&1
printf '%s\n' "$?" > "$U2_LAB/validator-ubuntu.exit"
