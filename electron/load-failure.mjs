// What to do when the main window cannot load Apple Music (offline, DNS failure, captive portal,
// bad certificate, proxy down). Without this the window stays a blank frame.

const FRIENDLY = new Map([
    [-106, 'You are offline'],
    [-105, 'Apple Music could not be found. Check your connection or DNS'],
    [-109, 'Apple Music is unreachable from this network'],
    [-102, 'The connection was refused'],
    [-118, 'The connection timed out'],
    [-7,   'The connection timed out'],
    [-130, 'The proxy server could not be reached'],
]);

// Main-frame failures only: sub-resource failures are normal on a flaky network, and -3 is an
// aborted load (a newer navigation replaced it), which is not a failure at all.
export function shouldShowOffline({ errorCode, isMainFrame, validatedURL = '' }) {
    if (!isMainFrame || errorCode === -3 || errorCode === 0) return false;
    if (validatedURL.startsWith('file:')) return false; // the offline page itself
    return true;
}

export function offlineHash(errorCode, description = '') {
    const text = FRIENDLY.get(errorCode)
        ?? (errorCode <= -200 && errorCode > -300 ? 'A secure connection to Apple Music could not be made' : 'Apple Music could not be loaded');
    const detail = `${description || 'error'} (${errorCode})`;
    return `m=${encodeURIComponent(text)}&d=${encodeURIComponent(detail)}`;
}
