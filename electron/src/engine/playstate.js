// The per-track playback attempt as a finite automaton. States and the transition relation are explicit so
// a move the design does not allow is detectable (and unit-testable) instead of silently accepted.
//
//   idle ──▶ opening ──▶ cdn-settling ──▶ streaming ──▶ complete
//     ▲         │              │              │            │
//     └─────────┴──────────────┴──────────────┴────────────┘   (reset / error: any state may fall back to idle)
//   opening ──▶ streaming also exists: sessions that need no CDN gate go straight to streaming.
export const PLAY_STATE = Object.freeze({
    IDLE:         'idle',          // no active attempt
    OPENING:      'opening',       // POST /api/v1/playback in flight
    CDN_SETTLING: 'cdn-settling',  // session open; CDN gate open so MK can fire NPIDF
    STREAMING:    'streaming',     // MSE source buffer active
    COMPLETE:     'complete',      // stream fully pumped
});

const S = PLAY_STATE;
export const PLAY_TRANSITIONS = Object.freeze({
    [S.IDLE]:         Object.freeze([S.OPENING]),
    [S.OPENING]:      Object.freeze([S.CDN_SETTLING, S.STREAMING, S.IDLE]),
    [S.CDN_SETTLING]: Object.freeze([S.STREAMING, S.IDLE]),
    [S.STREAMING]:    Object.freeze([S.COMPLETE, S.IDLE]),
    [S.COMPLETE]:     Object.freeze([S.IDLE]),
});

// Staying in a state is always fine (repeated resets, repeated progress); otherwise the move must be an edge.
export function isLegalPlayTransition(prev, next) {
    if (prev === next) return prev in PLAY_TRANSITIONS;
    return PLAY_TRANSITIONS[prev]?.includes(next) ?? false;
}
