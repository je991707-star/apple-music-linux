# Source from the U2 worktree; no system/global configuration changes.
U2_ROOT=/var/home/h97m/Projects/adam-u2-ordering
U2_LAB=${U2_LAB:-$U2_ROOT/.u2-lab/u2-3-20261102-run1}
U2_PORTABLE=/var/home/h97m/Projects/adam-portable-vlc/.portable-vlc-lab
U2_RUNTIME=$U2_PORTABLE/phase4c-20261029-run1/build-repeat/runtime-bundle
U2_GO=$U2_PORTABLE/phase3/toolchain/go/bin/go
export HOME=$U2_LAB/home TMPDIR=$U2_LAB/tmp XDG_CONFIG_HOME=$U2_LAB/config XDG_CACHE_HOME=$U2_LAB/cache
export GOCACHE=$U2_LAB/go-cache GOPATH=$U2_LAB/go-path GOMODCACHE=$U2_LAB/go-path/pkg/mod GOTOOLCHAIN=local GOWORK=off GOPROXY=off GOSUMDB=off
export CGO_ENABLED=1 CGO_CFLAGS=-I$U2_PORTABLE/phase3/headers/extracted/usr/include
export CGO_LDFLAGS="-L$U2_LAB/link -Wl,-rpath,$U2_RUNTIME"
export LD_LIBRARY_PATH=$U2_RUNTIME VLC_PLUGIN_PATH=$U2_RUNTIME/plugins
export ADAM_VLC_TEST_FIXTURE=/var/home/h97m/Projects/adam-isolated/.isolated-lab/vb6/fixtures/fragmented.mp4
mkdir -p "$HOME" "$TMPDIR" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$GOCACHE" "$GOPATH" "$U2_LAB/link"
if [ ! -e "$U2_LAB/link/libvlc.so" ]; then ln -s "$U2_RUNTIME/libvlc.so.5" "$U2_LAB/link/libvlc.so"; fi
