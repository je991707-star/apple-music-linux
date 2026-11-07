import crypto from 'node:crypto';
import {createAccountWorkspaceStore} from './engine-account-workspace.mjs';

// Process/workspace scope with Main-owned attempt correlation; never a Source grant.
// Production has no qualified Native verifier and cannot publish Native Ready.
// Only trusted Main receives these methods; there is no IPC approval endpoint.
export function createEngineAccountHost({parent, getLifecycle, onRevoke = () => {}, getTrustedBoot = null, verifyNativeCompletion = null, now = Date.now, sendAuthentication = null}) {
    const store = createAccountWorkspaceStore(parent), owners = new WeakMap(), contexts = new WeakMap();
    let epoch = 0, revision = 0, pending = false;
    function nextEpoch() { if (!Number.isSafeInteger(++epoch)) throw Error('Account epoch unavailable'); return epoch; }
    function record(context, live = true) {
        const r = contexts.get(context);
        if (!r || r.revoked || r.epoch !== epoch || (live && (!r.ready || !getLifecycle().isCurrent(r.owner)))) throw Error('Account process scope revoked');
        store.assert(r.workspace); return r;
    }
    function prepare(owner) {
        if (!owner || !getLifecycle().isOwned(owner) || owners.has(owner)) throw Error('Account owner already prepared');
        const workspace = store.create();
        const context = Object.freeze({ id: crypto.randomUUID(), workspaceId: workspace.id, accountEpoch: nextEpoch(), get nativeAuthenticated() { return authStatus(context).nativeAuthenticated; } });
        const r = { owner, workspace, context, epoch, bootNonce: crypto.randomBytes(32).toString('hex'), ready: false, revoked: false, auth: null, abort: new AbortController() };
        owners.set(owner, r); contexts.set(context, r); return context;
    }
    function revoke(owner, reason) {
        const r = owners.get(owner); if (!r || r.revoked) return;
        if (r.auth?.timer) clearTimeout(r.auth.timer);
        if (r.auth && !denied(r.auth.phase)) r.auth.phase = 'revoked';
        r.revoked = true; r.abort.abort(); store.revoke(r.workspace);
        if (r.epoch === epoch) nextEpoch();
        onRevoke(Object.freeze({ ownerId: owner.id, bootNonce: r.bootNonce, accountEpoch: r.epoch, reason }));
    }
    async function beginFresh({signal} = {}) {
        if (pending || signal?.aborted) throw Error('Fresh account process unavailable');
        pending = true;
        const cancel = () => { void getLifecycle().suspend().catch(() => {}); };
        signal?.addEventListener('abort', cancel, {once:true});
        try {
            const owner = await getLifecycle().restart();
            if (signal?.aborted) throw Error('Fresh account process cancelled');
            const r = owners.get(owner); if (!r) throw Error('Fresh account process unconfirmed');
            record(r.context); return r.context;
        } finally { signal?.removeEventListener('abort', cancel); pending = false; }
    }
    const denied = phase => ['failed','cancelled','expired','revoked'].includes(phase);
    const terminal = phase => phase === 'completed' || denied(phase);
    const sameBinding = (a,b) => !!a && !!b && Object.keys(a).length === Object.keys(b).length && Object.keys(a).every(k => Object.hasOwn(b,k) && a[k] === b[k]);
    function bootFor(r) {
        record(r.context);
        if (typeof getTrustedBoot !== 'function') throw Error('Trusted Engine Boot unavailable');
        const boot = getTrustedBoot();
        if (!boot || boot.nonce !== r.bootNonce || !/^[a-f0-9]{32}$/.test(boot.boot) || !Number.isSafeInteger(boot.pid) || boot.pid <= 0) throw Error('Trusted Engine Boot unavailable');
        return boot;
    }
    function authStatus(context) {
        const r = contexts.get(context); if (!r) throw Error('Unknown account process');
        const a = r.auth;
        if (a && !denied(a.phase) && now() >= a.binding.expiresAt) {
            a.phase = 'expired'; revoke(r.owner, 'authentication-expired');
            void getLifecycle().suspend().catch(() => {});
        }
        let live = false;
        try {
            record(context); live = true;
            if (a?.phase === 'completed') { const boot = bootFor(r); if (boot.boot !== a.binding.engineBoot || boot.nonce !== a.binding.bootNonce) throw Error('Auth Boot changed'); }
        } catch { live = false; }
        if (a && !live && !denied(a.phase)) a.phase = 'revoked';
        return Object.freeze({phase: a?.phase ?? 'idle', nativeAuthenticated: live && a?.phase === 'completed', binding: a?.binding ?? null, blockedReason: a?.blockedReason ?? null, inputChallenge: live && a?.phase === 'challenge_required' && a.challenge?.expiresAt > now() ? a.challenge : null});
    }
    function beginAuthentication(context, {timeoutMs = 120000} = {}) {
        const r = record(context);
        if (r.auth || !Number.isSafeInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 600000) throw Error('Fresh Engine required for authentication');
        const boot = bootFor(r);
        if (!Number.isSafeInteger(++revision)) throw Error('Credential revision unavailable');
        const binding = Object.freeze({accountId:context.id, accountEpoch:r.epoch, attemptId:crypto.randomUUID(), engineOwnerId:r.owner.id, engineBoot:boot.boot, bootNonce:r.bootNonce, credentialRevision:revision, generation:revision, expiresAt:now()+timeoutMs});
        r.auth = {phase:'accepted',binding,blockedReason:null,consumed:false};
        return binding; // Main-only object identity; a JSON copy is not an attempt handle.
    }
    async function observeAuthentication(context, attempt, response) {
        let r, a;
        const current = (allowCompleted = false) => {
            r = record(context); a = r.auth;
            if (!a || a.binding !== attempt || denied(a.phase) || (!allowCompleted && a.phase === 'completed') || now() >= attempt.expiresAt) { authStatus(context); return false; }
            const boot = bootFor(r);
            return boot.boot === attempt.engineBoot && boot.nonce === attempt.bootNonce;
        };
        try {
            if (!current(true) || response?.status !== 200) return false; // 202 is acceptance only.
            const body = response.body;
            if (!sameBinding(body?.authBinding, attempt)) return false;
            const phases = {accepted:'accepted',logging_in:'running',challenging:'challenge_required',logged_in:'completed',failed:'failed',cancelled:'cancelled',expired:'expired',revoked:'revoked',logged_out:'revoked'};
            const phase = phases[body.state?.authentication];
            if (!phase) return false;
            if (phase !== 'completed') { a.phase = a.phase === 'completed' && !denied(phase) ? 'revoked' : phase; return false; }
            if (a.consumed) return false;
            const evidence = body.authEvidence;
            if (body.state.process !== 'running' || body.nativeAuthenticated !== true || evidence?.kind !== 'native-qualified' || typeof evidence.id !== 'string' || !evidence.id || evidence.id.length > 128 || typeof verifyNativeCompletion !== 'function') {
                a.phase = 'running'; a.blockedReason = 'native-completion-unqualified'; return false;
            }
            // The verifier is a Main-owned dependency, never IPC, a Renderer flag,
            // a status string or an environment switch. Production installs none.
            const verified = await verifyNativeCompletion(Object.freeze({...attempt}), Object.freeze({...evidence}));
            if (verified !== true || !current()) return false;
            a.phase = 'completed'; a.consumed = true; a.blockedReason = null; return true;
        } catch { return false; }
    }
    const staleReply = () => Object.assign(Error('Stale private authentication response'), {code:'STALE_AUTH'});
    function stillOwned(r) {
        try { return record(r.context) === r && getLifecycle().currentOwner() === r.owner; } catch { return false; }
    }
    async function sendBound(context, attempt, operation, extra = {}) {
        const r = record(context);
        if (!r.auth || r.auth.binding !== attempt || typeof sendAuthentication !== 'function') throw Error('Private authentication unavailable');
        const boot = bootFor(r);
        if (boot.boot !== attempt.engineBoot || boot.nonce !== attempt.bootNonce) throw staleReply();
        const response = await sendAuthentication(Object.freeze({operation,binding:attempt,...extra}), r.abort.signal);
        if (!stillOwned(r) || r.auth.binding !== attempt || (denied(r.auth.phase) && !['cancel','revoke'].includes(operation))) throw staleReply();
        const after = bootFor(r);
        if (after.boot !== attempt.engineBoot || after.nonce !== attempt.bootNonce) throw staleReply();
        if (response?.status !== (operation === 'begin' ? 202 : 200)) throw Error('Private authentication rejected');
        if (!sameBinding(response.body?.authBinding,attempt)) throw staleReply();
        const state = response.body.state?.authentication;
        if (response.body.nativeAuthenticated !== false || response.body.authEvidence || !['accepted','logging_in','challenging','failed','cancelled','expired','revoked'].includes(state)) throw Error('Private authentication protocol rejected');
        const challenge = response.body.inputChallenge;
        if (challenge && (!/^[a-f0-9]{32}$/.test(challenge.id) || challenge.type !== 'two_factor' || !Number.isSafeInteger(challenge.expiresAt) || challenge.expiresAt <= now() || challenge.expiresAt > attempt.expiresAt || state !== 'challenging')) throw Error('Private challenge rejected');
        if (!denied(r.auth.phase)) r.auth.challenge = challenge ? Object.freeze({id:challenge.id,type:challenge.type,expiresAt:challenge.expiresAt}) : null;
        return response;
    }
    async function requestAuthentication(context, options = {}) {
        if (typeof sendAuthentication !== 'function') throw Error('Private authentication transport unavailable');
        const attempt = beginAuthentication(context,options), r = record(context), a = r.auth;
        a.wireOpen = true;
        const expire = () => {
            if (!stillOwned(r) || denied(a.phase)) return;
            const remaining = attempt.expiresAt-now();
            if (remaining > 0) { a.timer = setTimeout(expire,remaining); a.timer.unref?.(); return; }
            void endAuthentication(context,'expired','authentication-expired').catch(() => {});
        };
        a.timer = setTimeout(expire,Math.max(1,attempt.expiresAt-now()));
        a.timer.unref?.();
        try { await sendBound(context,attempt,'begin'); return attempt; }
        catch (error) {
            // A's late reply must never stop B. Foreign-scope replies are ignored;
            // a failure of the current owned transport revokes only that process.
            if (error.code !== 'STALE_AUTH' && stillOwned(r)) await endAuthentication(context,now()>=attempt.expiresAt?'expired':'failed','authentication-request-failed');
            throw error;
        }
    }
    async function pollAuthentication(context, attempt) {
        const response = await sendBound(context,attempt,'status');
        await observeAuthentication(context,attempt,response);
        return authStatus(context);
    }
    async function handoffInput(context, attempt, operation, value, challengeId) {
        const r = record(context), a = r.auth, state = authStatus(context);
        if (!a || a.binding !== attempt || denied(state.phase) || now() >= attempt.expiresAt || a.inputBusy) throw Error('Private input unavailable');
        if (operation === 'credentials' && (a.inputUsed || !['accepted','running'].includes(state.phase))) throw Error('Private input unavailable');
        if (operation === 'challenge' && (!a.challenge || state.phase !== 'challenge_required' || a.challenge.id !== challengeId || a.challenge.expiresAt <= now())) throw Error('Private input unavailable');
        const fields = operation === 'credentials' ? ['email','password'] : ['reply'];
        if (!value || Object.getPrototypeOf(value) !== Object.prototype || Object.keys(value).length !== fields.length || !fields.every(k => typeof value[k] === 'string')) throw Error('Invalid private input');
        const owned = {}, encoded = {};
        try {
            for (const k of fields) {
                if (value[k].length > (k === 'email' ? 256 : k === 'password' ? 255 : 6) || value[k].includes('\0') || !value[k].isWellFormed()) throw Error('Invalid private input');
                owned[k] = Buffer.from(value[k], 'utf8');
                if (!owned[k].length || owned[k].length > (k === 'email' ? 256 : k === 'password' ? 255 : 6) || (k === 'reply' && !/^[0-9]{6}$/.test(value[k]))) throw Error('Invalid private input');
                encoded[k] = owned[k].toString('base64');
            }
            a.inputBusy = true;
            if (operation === 'credentials') a.inputUsed = true;
            else a.challenge = null; // reserve before await; never replay after lost acknowledgement
            const response = await sendBound(context,attempt,operation,{input:encoded,...(challengeId ? {challengeId} : {})});
            if (response.body.inputState !== 'consumed') throw Error('Private input rejected');
            await observeAuthentication(context,attempt,response);
            return Object.freeze({ok:true,nativeAuthenticated:false});
        } catch {
            if (a.inputBusy && stillOwned(r)) await endAuthentication(context,'failed','private-input-failed').catch(() => {});
            throw Error('Private input unavailable');
        } finally {
            a.inputBusy = false;
            for (const v of Object.values(owned)) v.fill(0);
            for (const k of fields) encoded[k] = ''; // JS strings/JSON/TLS copies cannot be guaranteed erased
        }
    }
    async function endAuthentication(context, phase, reason) {
        const r = record(context), a = r.auth;
        if (a) { a.challenge = null; a.phase = phase; if (a.timer) clearTimeout(a.timer); }
        try {
            if (a?.wireOpen && typeof sendAuthentication === 'function') await sendBound(context,a.binding,phase==='revoked'?'revoke':'cancel');
        } catch {} finally {
            revoke(r.owner,reason);
            if (getLifecycle().currentOwner() === r.owner) await getLifecycle().suspend();
        }
    }
    return { prepare, revoke, beginFresh, beginAuthentication, observeAuthentication, authStatus, requestAuthentication, pollAuthentication,
        handoffCredential(context,attempt,value) { return handoffInput(context,attempt,'credentials',value); },
        handoffChallenge(context,attempt,challengeId,reply) { return handoffInput(context,attempt,'challenge',{reply},challengeId); },
        revokeAuthentication(context) { return endAuthentication(context, 'revoked', 'authentication-revoked'); },
        invalidateCredentials(context) { return endAuthentication(context, 'revoked', 'credential-changed'); },
        owned(owner) { const r = owners.get(owner); if (!r || r.revoked) throw Error('Account owner revoked'); r.ready = true; record(r.context); },
        exited(owner) { const r = owners.get(owner); if (r) { revoke(owner, 'engine-exited'); store.release(r.workspace); } },
        assertCurrent(context) { record(context); },
        assertPrepared(context) { record(context, false); },
        paths(context) { return store.paths(record(context, false).workspace); },
        environment(context, runtimeLib64Dir) { const r=record(context,false); return {...store.environment(r.workspace,runtimeLib64Dir), ADAM_MAIN_AUTH_ACCOUNT:context.id, ADAM_MAIN_AUTH_EPOCH:String(r.epoch), ADAM_MAIN_AUTH_OWNER:r.owner.id}; },
        bootNonce(context) { return record(context, false).bootNonce; },
        writeConfig(context, port, runtimeLib64Dir) { store.writeConfig(record(context, false).workspace, port, runtimeLib64Dir); },
        current() { const owner = getLifecycle().currentOwner(), r = owner && owners.get(owner); if (!r) throw Error('No current account process'); record(r.context); return r.context; },
        async run(context, fn) { const r = record(context); const result = await fn(Object.freeze({ signal: r.abort.signal, workspaceId: r.workspace.id, accountEpoch: r.epoch })); record(context); return result; },
        async cancel(context) { return endAuthentication(context, 'cancelled', 'cancelled'); },
        reuse() { throw Error('Cross-process account session reuse is unqualified'); },
        close() { const owner=getLifecycle()?.currentOwner();const r=owner&&owners.get(owner);if(r?.auth?.timer)clearTimeout(r.auth.timer);store.close(); } };
}
