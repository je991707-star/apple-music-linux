#!/bin/sh
# Adam — self-extracting installer
# Usage: ./adam.run [--force] [--system] [--uninstall]
#
# --force      reinstall even if already at this version
# --system     install to /opt/adam + /usr/local/bin (needs sudo)
# --uninstall  remove a previous installation

VERSION="__VERSION__"

# ── defaults (user install) ───────────────────────────────────────────────────
INSTALL_DIR="${XDG_DATA_HOME:-$HOME/.local/lib}/adam"
BIN_DIR="$HOME/.local/bin"
DESKTOP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"

FORCE=0
SYSTEM=0
UNINSTALL=0

# ── launcher directory ───────────────────────────────────────────────────────
# The command is installed as `adam`. The standard home for user executables is
# $HOME/.local/bin (XDG Base Directory spec 0.8, systemd file-hierarchy(7)); putting it on PATH
# is the distribution's job, so this installer does not edit shell startup files: the shell and
# how it is configured are the user's business, and the installer cannot know them. The one
# adaptation is to the user's *current* PATH: if ~/.local/bin is not on it but ~/bin (the older
# convention) is, the launcher goes there so the command works immediately.
on_path() { case ":$PATH:" in *":$1:"*) return 0 ;; esac; return 1; }

for arg in "$@"; do
    case "$arg" in
        --force)    FORCE=1 ;;
        --system)   SYSTEM=1 ;;
        --uninstall) UNINSTALL=1 ;;
        --help|-h)
            echo "Usage: $0 [--force] [--system] [--uninstall]"
            exit 0 ;;
        *) echo "Unknown option: $arg"; exit 1 ;;
    esac
done

if [ "$SYSTEM" = "1" ]; then
    INSTALL_DIR="/opt/adam"
    BIN_DIR="/usr/local/bin"
    DESKTOP_DIR="/usr/share/applications"
    ICON_DIR="/usr/share/icons/hicolor/256x256/apps"
fi

if [ "$SYSTEM" != "1" ] && ! on_path "$BIN_DIR" && [ -d "$HOME/bin" ] && [ -w "$HOME/bin" ] && on_path "$HOME/bin"; then
    BIN_DIR="$HOME/bin"
fi

# ── uninstall ─────────────────────────────────────────────────────────────────
if [ "$UNINSTALL" = "1" ]; then
    rm -rf "$INSTALL_DIR"
    for d in "$BIN_DIR" "$HOME/.local/bin" "$HOME/bin"; do
        # only a launcher this installer wrote (it execs the installed binary)
        if [ -f "$d/adam" ] && grep -qF "$INSTALL_DIR/adam" "$d/adam" 2>/dev/null; then
            rm -f "$d/adam"
        fi
    done
    rm -f  "$DESKTOP_DIR/adam.desktop"
    rm -f  "$ICON_DIR/adam.png"
    update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
    echo "Uninstalled Adam."
    exit 0
fi

# ── version guard ─────────────────────────────────────────────────────────────
if [ "$FORCE" = "0" ] && [ -f "$INSTALL_DIR/.version" ]; then
    installed=$(cat "$INSTALL_DIR/.version")
    if [ "$installed" = "$VERSION" ]; then
        echo "Adam $VERSION is already installed."
        echo "Run: adam   or pass --force to reinstall."
        exit 0
    fi
fi

# ── zstd check ───────────────────────────────────────────────────────────────
if ! command -v zstd >/dev/null 2>&1; then
    echo "Error: zstd is required for installation."
    echo "  Debian/Ubuntu:  sudo apt install zstd"
    echo "  Fedora/RHEL:    sudo dnf install zstd"
    echo "  Arch:           sudo pacman -S zstd"
    echo "  openSUSE:       sudo zypper install zstd"
    exit 1
fi

# ── extract ───────────────────────────────────────────────────────────────────
# Payload starts after the __PAYLOAD_BELOW__ sentinel line.
# Byte-offset method (binary-safe): count bytes through that line, then tail -c.
SENTINEL_LINE=$(grep -an '^__PAYLOAD_BELOW__$' "$0" | tail -1 | cut -d: -f1)
HEADER_BYTES=$(head -n "$SENTINEL_LINE" "$0" | wc -c)
PAYLOAD_START=$((HEADER_BYTES + 1))

echo "Installing Adam $VERSION..."
mkdir -p "$INSTALL_DIR"

tail -c +"$PAYLOAD_START" "$0" \
    | zstd -d --long=31 -q \
    | tar -x --strip-components=1 -C "$INSTALL_DIR"

# ── permissions ───────────────────────────────────────────────────────────────
chmod +x \
    "$INSTALL_DIR/adam" \
    "$INSTALL_DIR/chrome_crashpad_handler" \
    "$INSTALL_DIR/resources/engine"

# chrome-sandbox: needs setuid root for the Chromium sandbox.
# Without it we fall back to --no-sandbox in the launcher (safe for local use).
if [ "$SYSTEM" = "1" ] && [ "$(id -u)" = "0" ]; then
    chown root:root "$INSTALL_DIR/chrome-sandbox"
    chmod 4755      "$INSTALL_DIR/chrome-sandbox"
fi

# ── launcher ──────────────────────────────────────────────────────────────────
mkdir -p "$BIN_DIR"
cat > "$BIN_DIR/adam" <<EOF
#!/bin/sh
exec "$INSTALL_DIR/adam" --no-sandbox "\$@"
EOF
chmod +x "$BIN_DIR/adam"

# ── desktop entry + icon ──────────────────────────────────────────────────────
mkdir -p "$DESKTOP_DIR" "$ICON_DIR"

# Resolve the best app icon: bundled icon.png > tray-icon.png fallback
APP_ICON="$INSTALL_DIR/resources/icon.png"
if [ ! -f "$APP_ICON" ]; then
    APP_ICON="$INSTALL_DIR/resources/tray-icon.png"
fi

# Copy to hicolor theme dir (for DEs that resolve icon names)
cp "$APP_ICON" "$ICON_DIR/adam.png" 2>/dev/null || true
gtk-update-icon-cache -f -t "$HOME/.local/share/icons/hicolor" 2>/dev/null || true

# Use absolute path for Icon= — bypasses icon theme cache entirely,
# guaranteed to show the correct icon regardless of DE cache state.
# (Freedesktop spec: absolute paths are used directly, no theme lookup.)
cat > "$DESKTOP_DIR/adam.desktop" <<EOF
[Desktop Entry]
Name=Adam
GenericName=Music Player
Comment=Apple Music desktop client for Linux and Windows
Exec=$INSTALL_DIR/adam --no-sandbox %u
Icon=$APP_ICON
Type=Application
Categories=AudioVideo;Music;Network;
StartupWMClass=Adam
MimeType=x-scheme-handler/adam;x-scheme-handler/music;x-scheme-handler/musics;x-scheme-handler/itms;x-scheme-handler/itmss;x-scheme-handler/itunes;x-scheme-handler/itals;x-scheme-handler/italss;x-scheme-handler/itvls;x-scheme-handler/itvlss;x-scheme-handler/itsradio;x-scheme-handler/itunesradio;x-scheme-handler/daap;x-scheme-handler/itpc;x-scheme-handler/ame;
Keywords=music;apple;streaming;
EOF

update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
xdg-mime default adam.desktop x-scheme-handler/adam 2>/dev/null || true

# ── version stamp ─────────────────────────────────────────────────────────────
echo "$VERSION" > "$INSTALL_DIR/.version"

echo ""
echo "Adam $VERSION installed to $INSTALL_DIR"
echo "Launcher: $BIN_DIR/adam"

if ! on_path "$BIN_DIR"; then
    echo ""
    echo "Note: $BIN_DIR is not on your PATH in this session, so 'adam' will not be"
    echo "found by name yet. It is the standard per-user location for commands, and most systems"
    echo "add it automatically at your next login once the directory exists. If the command is"
    echo "still not found after logging in again, add $BIN_DIR to PATH in your shell's startup file."
    echo "Until then:  $BIN_DIR/adam"
fi

exit 0
__PAYLOAD_BELOW__
