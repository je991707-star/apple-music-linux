import crypto from 'node:crypto';
import {createMainSourceHost} from './playback-main-source.mjs';
// This is a production Main principal, NOT a fixture owner or a login identity.
// No source grant may be minted by Renderer input. A qualified Session Factory
// must supply Engine-bound proofs before any source operation becomes available.
export function createMainPlaybackPrincipal({ guard, enabled, send, descriptor, token, issuer, sourceSend, stream, deliver, onUnconfirmedCleanup }) {
    const principalId = crypto.randomUUID();
    const operations = new Set(['time','status','control','load','seek','pause','resume','stop','intent','touch','reopen','create','release','metadata','setVolume','library','artwork','subscribe','eventAck','unsubscribe','prewarm']);
    const sourceHost=sourceSend?createMainSourceHost({guard,descriptor,send:sourceSend,token,issuer,principalId,stream,deliver,onUnconfirmedCleanup}):null;
    const denied = () => ({ status: 403, body: { error: 'source-authorization-unavailable', mayHaveApplied: false } });
    async function execute(event, request) {
        const scope = guard.capture(event);
        if (!enabled) throw Error('Ordered playback unavailable');
        if (!request || Object.getPrototypeOf(request) !== Object.prototype ||
            Object.keys(request).some(k => !['operation','body'].includes(k)) ||
            !operations.has(request.operation)) throw Error('Invalid fixed playback operation');
        if (Buffer.byteLength(JSON.stringify(request)) > 4096) throw Error('Playback request limit');
        // Do not use the API token as per-source authority; no real source factory
        // has been qualified on this integration branch. In particular, a known
        // Session ID or forged lease is NOT an ownership claim.
        if(sourceHost && (sourceHost.owns() || !['time','status'].includes(request.operation))) return sourceHost.execute(event,request);
        if (!['time','status'].includes(request.operation) || request.body !== undefined) return denied();
        try {
            const trust = descriptor();
            const result = await send(trust, token);
            guard.assertCurrent(scope);
            const current = descriptor();
            if (current.boot !== trust.boot || current.pin !== trust.pin || current.nonce !== trust.nonce) throw Error('Engine changed');
            // Only the minimum control identity is visible. No other Session's
            // metadata, URLs, lease, credentials or raw server errors cross IPC.
            const data = result.body;
            if (result.status !== 200 || data?.control?.boot !== trust.boot ||
                typeof data.control.epoch !== 'string' || !/^(?:[a-f0-9]{32})?$/.test(data.control.epoch) ||
                typeof data.control.versioned !== 'boolean') throw Error('Unconfirmed status');
            return { status: 200, body: { control: { boot: trust.boot, epoch: data.control.epoch,
                versioned: data.control.versioned === true }, state: 'unowned', principal: principalId } };
        } catch { throw Error('Playback transport unconfirmed; rollback is not implied'); }
    }
    return { execute, principalId, sourceHost };
}
