# U2-3 Controller integration

Production functions are extracted from the current `engine-playback.js` AST;
only UI/timer/source-provider boundaries are controlled. Assertions retain U1's
seven contracts. `crosslayer.mjs` additionally connects these same functions to
real Go handlers, Session Manager, StreamingReader and the pinned libVLC via a
private loopback test server (`vlc_ordering_test`, Go `_test.go` only).

This is **not** normal MusicKit / Main IPC / TLS / account qualification. The
Node driver owns the synthetic API header; it is never exposed to a web page.
The production opt-in protocol has no raw-fetch fallback if a source-authorized
Main bridge is unavailable. Legacy remains the default. See the current Lab's
MAIN-AUTHORIZATION-GAPS.md before enabling any normal UI bridge.

To reproduce without overwriting historical evidence, follow REPRODUCE.md in
`.u2-lab/u2-3-20261102-run1`. Do not run historical one-time port tools against
current source. The Lab preserves baseline source and original RED logs.
