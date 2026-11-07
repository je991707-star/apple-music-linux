// Fixed playback protocol. Authentication / principal validation belong to Main.
// A private test client may inject orderedFetch; production has no generic proxy.
export function createPlaybackCommands({ fetch, orderedFetch, origin, scope, enabled = false,
    requestID = () => crypto.randomUUID() }) {
    let connection = null, connecting = null, connectionSession = null, sequence = 0, active = null, connectionRevision = 0;
    const superseded = () => new Error('Playback command superseded');
    const ownership = () => ({ ...scope() });
    function current(issued, owner, type) {
        if (sequence !== issued) throw superseded();
        if (type === 'stop') return; // Stop keeps its captured owner during reset.
        const now = scope();
        if (Object.keys(owner).some(k => now[k] !== owner[k])) throw superseded();
    }
    function route(url, options, status = false) {
        const u = new URL(url);
        const type = u.pathname.split('/').pop();
        if (u.origin !== origin || u.username || u.password || u.search || u.hash ||
            u.pathname !== `/api/v1/vlc/${type}` ||
            !(status ? type === 'time' : ['load', 'seek', 'pause', 'resume', 'stop', 'intent'].includes(type)) ||
            (options.method || (status ? 'GET' : 'POST')) !== (status ? 'GET' : 'POST')) {
            throw new Error('Invalid fixed playback route');
        }
        return type;
    }
    function bridge() {
        if (typeof orderedFetch !== 'function') throw new Error('Authenticated playback bridge unavailable');
        return orderedFetch;
    }
    async function decode(r) {
        let data;
        try { data = await r.json(); } catch { throw new Error('Invalid playback response'); }
        if (!r.ok) { const e = new Error(`Playback control rejected (${r.status})`); e.status = r.status; e.mayHaveApplied = data?.mayHaveApplied === true; throw e; }
        return data;
    }
    async function connect(session) {
        if (connection) return connection;
        if (!connecting) {
            const revision = connectionRevision;
            const pending = (async () => {
                const c = (await decode(await bridge()(`${origin}/api/v1/vlc/time`, { method: 'GET' }))).control;
                if (!c?.boot || typeof c.epoch !== 'string') throw new Error('Engine control identity unavailable');
                const claimed = await decode(await bridge()(`${origin}/api/v1/vlc/control`, {
                    method: 'POST', body: JSON.stringify({ boot: c.boot, epoch: c.epoch, sessionId: session }),
                }));
                if (revision !== connectionRevision) throw superseded();
                if (claimed.boot !== c.boot || !claimed.epoch) throw new Error('Engine claim identity mismatch');
                connection = claimed;
                connectionSession = session;
                return connection;
            })();
            const wrapped = pending.finally(() => { if (connecting === wrapped) connecting = null; });
            connecting = wrapped;
        }
        return connecting;
    }
    async function request(url, options = {}) {
        const type = route(url, options);
        if (!enabled) return fetch(url, options);
        const owner = ownership(), body = options.body ? JSON.parse(options.body) : {};
        const target = type === 'stop' ? (active || owner) : owner;
        const sessionId = body.sessionId || target.sessionId, loadGeneration = target.loadGeneration;
        const issued = ++sequence, requestId = requestID();
        if (type === 'load' && connection && connectionSession !== sessionId) {
            connectionRevision++;connection=null;connecting=null;connectionSession=null;
        }
        if (type === 'load' && connection) {
            const c = (await decode(await bridge()(`${origin}/api/v1/vlc/time`, { method: 'GET' }))).control;
            current(issued, owner, type);
            if (!c?.boot) throw new Error('Engine control identity unavailable');
            if (c.boot !== connection.boot) { connectionRevision++; connection = null; connecting = null; }
            else if (c.epoch !== connection.epoch) throw new Error('Playback epoch revoked');
        }
        const c = await connect(sessionId);
        current(issued, owner, type);
        if (options.signal?.aborted) throw new Error('Playback request aborted; rollback is not implied');
        if (!sessionId || !Number.isSafeInteger(loadGeneration) || loadGeneration < 0) throw new Error('Missing playback ownership');
        const command = { boot: c.boot, epoch: c.epoch, sessionId, loadGeneration, sequence: issued, type, requestId };
        if (type === 'load') active = { sessionId, loadGeneration };
        const response = await bridge()(url, { ...options, method: 'POST', body: JSON.stringify({ ...body, sessionId, command }) });
        current(issued, owner, type);
        // Preserve error status for the Controller's owner-checked 404 decision.
        if (!response.ok) return response;
        const data = await decode(response.clone ? response.clone() : response);
        current(issued, owner, type);
        if (options.signal?.aborted) throw new Error('Playback response obsolete; rollback is not implied');
        if (!data.command || Object.keys(command).some(k => data.command[k] !== command[k])) throw new Error('Playback ACK identity mismatch');
        const stage = { load: 'media-committed', seek: 'seek-dispatched', intent: 'intent-invalidated', pause: 'native-committed', resume: 'native-committed', stop: 'native-committed' }[type];
        if (data.ack !== stage) throw new Error('Playback ACK stage mismatch');
        if (type === 'stop') active = null;
        return response.clone ? response : { ...response, json: async () => data };
    }
    async function status(url, options = {}) {
        route(url, options, true);
        if (!enabled) return fetch(url, options);
        const r = await bridge()(url, { ...options, method: 'GET' });
        if (!r.ok) return r;
        const data = await decode(r.clone ? r.clone() : r);
        return r.clone ? r : { ...r, json: async () => data };
    }
    return { enabled, request, status,
        prepare: async session => {
            if (!enabled || typeof session !== 'string' || !session) throw new Error('Missing ordered session');
            if (connectionSession !== session) {connectionRevision++;connection=null;connecting=null;connectionSession=null;}
            return {...await connect(session)};
        }, identity: () => connection && { boot: connection.boot, epoch: connection.epoch, revision: connectionRevision },
        barrier: async () => {
            if (!enabled) return;
            const r = await request(`${origin}/api/v1/vlc/intent`, { method: 'POST' });
            if (!r.ok) throw new Error(`Playback intent rejected (${r.status})`);
        },
        stop: () => enabled && active ? request(`${origin}/api/v1/vlc/stop`, { method: 'POST' }) : Promise.resolve(),
    };
}
