// Renderer side of Sign Out / Forget This Device: ask the engine, then hand the
// result to the main process (which records a backstop only if the engine route
// failed), and turn the outcome into what Settings shows.

export const SIGN_OUT_TEXT = {
    marker: "Couldn't sign out: the engine isn't responding and the sign-out couldn't be recorded. Try again.",
    credentials: "Signed out of the engine, but the saved sign-in couldn't be removed. Try again.",
    durability: 'Signed out. If Adam is still signed in after a restart, sign out again.',
};

// Above the engine's bounded sign-out (45 s route + 2 s exit): when this gives up,
// the engine has finished or exited, so the backstop reload cannot overlap it.
export const SIGN_OUT_TIMEOUT_MS = 60_000;

/**
 * @param {{ engine: string, scope?: 'account'|'device', fetchFn?: Function,
 *           clearCredentials?: Function, timeoutMs?: number }} o
 * @returns {Promise<{ ok: boolean, message?: string, note?: string }>}
 */
export async function signOutFlow({ engine, scope = 'account', fetchFn = fetch, clearCredentials, timeoutMs = SIGN_OUT_TIMEOUT_MS }) {
    const route = scope === 'device' ? 'forget-device' : 'logout';
    const r = await fetchFn(`${engine.replace(/\/+$/, '')}/api/v1/drm/${route}`, {
        method: 'POST', signal: AbortSignal.timeout(timeoutMs),
    }).catch(() => null);
    const engineOk = !!r?.ok;
    if (!clearCredentials) return engineOk ? { ok: true } : { ok: false, message: SIGN_OUT_TEXT.marker };
    const res = await clearCredentials({ reload: true, backstop: engineOk ? null : scope })
        .catch(() => ({ ok: false, error: 'marker' }));
    if (!res?.ok) return { ok: false, message: res?.error === 'credentials' ? SIGN_OUT_TEXT.credentials : SIGN_OUT_TEXT.marker };
    return { ok: true, note: res.warning === 'durability' ? SIGN_OUT_TEXT.durability : '' };
}
