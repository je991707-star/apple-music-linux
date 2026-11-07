# Adam U2-27 — Public Source Snapshot

Date: 2026-11-07

Source checkpoint:
3260867a584048116ed8892973b315d06d05fbf8

This is a standalone source snapshot with a new Git root commit.
It does not preserve the original development commit history.

## Changes for public archival

- Removed embedded Widevine device private key and client identity.
- Disabled the embedded Widevine fallback identity.
- Replaced one malformed private-key test string.
- Removed embedded default Last.fm API credentials.
- Preserved seven U2 native credential source files.

## Status

U2 Ordered Mode is under development.
Native credential integration and production playback remain blocked
pending further verification.

This snapshot has not passed full compilation, integration,
authentication, playback, or release acceptance testing.

Historical test evidence and the original development history are
preserved separately in the user's local backups.
