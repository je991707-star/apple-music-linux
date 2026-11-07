const music = value => {
    try { const u = new URL(value); return u.origin === 'https://music.apple.com' && !u.username && !u.password; }
    catch { return false; }
};
// Frame identity alone survives some reloads. A captured record is revoked at
// navigation start, before any pending IPC result is allowed to return.
export function createMainDocumentGuard(getWindow) {
    const records = new WeakMap(), listeners = new Set();
    function attach(wc) {
        let generation = 0;
        const revoke = () => { const old=records.get(wc); records.set(wc, { generation: ++generation, ready: false }); if(old) for(const fn of listeners)fn(old); };
        revoke();
        wc.on('did-start-navigation', details => { if (details?.isMainFrame && !details.isSameDocument) revoke(); });
        wc.on('did-finish-load', () => { const r = records.get(wc); if (r) r.ready = music(wc.getURL()); });
        wc.on('render-process-gone', revoke);
        wc.on('destroyed', () => {revoke();records.delete(wc)});
    }
    function assertCurrent(scope) {
        const win = getWindow(), wc = scope.wc;
        if (!win || win.isDestroyed() || win.webContents !== wc || wc.isDestroyed() ||
            records.get(wc) !== scope.record || !scope.record?.ready || wc.isLoadingMainFrame() ||
            wc.mainFrame !== scope.frame || !music(wc.getURL()) || !music(scope.frame?.url)) throw Error('Playback document revoked');
    }
    function capture(event) {
        const s = { wc: event.sender, frame: event.senderFrame, record: records.get(event.sender) };
        assertCurrent(s); return s;
    }
    return { attach, capture, assertCurrent, onRevoke(fn) {listeners.add(fn);return()=>listeners.delete(fn)} };
}
