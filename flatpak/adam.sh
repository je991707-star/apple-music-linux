#!/bin/sh
# zypak runs Electron's Chromium sandbox through the Flatpak sandbox.
exec zypak-wrapper.sh /app/adam/adam --ozone-platform-hint=auto "$@"
