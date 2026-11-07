import {validateOwnMetadata,validateOwnEvent,validatePublicLibrary,validateFixedArtwork} from './scoped-source-policy.mjs';
import crypto from 'node:crypto';

// Main-only source authority. Approving an asset is NOT exposed through IPC.
// Source issuance/retirement is serialized; Native commands remain independently
// versioned and do not wait behind media preparation in this queue.
export function createMainSourceHost({guard,descriptor,send,token,issuer,principalId,stream,deliver=()=>{},onUnconfirmedCleanup=()=>{}}) {
    const mainGeneration=crypto.randomUUID();
    let revision=0,current=null,approved=null,adapter=null;
    let lifecycle=Promise.resolve(),blockedBoot=null;
    const subscriptions=new Map();
    function closeSub(sub,reason='closed') {
        if(sub.closed)return;sub.closed=true;clearTimeout(sub.timer);clearTimeout(sub.renewTimer);sub.streamGeneration++;subscriptions.delete(sub.id);sub.connection?.cancel();
        try {guard.assertCurrent(sub.scope);deliver(sub.scope,{subscriptionId:sub.id,closed:reason,lostBoundary:sub.lossBoundary??null});}catch{}
    }
    function closeSubscriptions(reason){for(const sub of subscriptions.values())closeSub(sub,reason);}
    function flushSub(sub){
        if(!sub.active||sub.closed)return;
        try {check(sub.scope,sub.trust,sub.owner);if(current!==sub.owned)throw Error('Source changed');
            for(const item of sub.pending){if(!item.sent){validateOwnEvent(item.value,sub.owned.grant,0);item.sent=true;deliver(sub.scope,{subscriptionId:sub.id,event:item.value,streamGeneration:item.generation,resynchronized:item.resynchronized===true,continuity:item.resynchronized?'authorized-snapshot':undefined,recoveryBoundary:item.recoveryBoundary});}}
        }catch{closeSub(sub,'authority-revoked');}
    }
    function deadlineSub(sub){
        clearTimeout(sub.timer);sub.timer=null;
        if(sub.pending.length||!sub.active)sub.timer=setTimeout(()=>closeSub(sub,'backpressure'),2000);
    }
    function assertSub(sub){check(sub.scope,sub.trust,sub.owner);if(sub.closed||current!==sub.owned||sub.epoch!==sub.owned.grant.epoch)throw Error('Subscription authority changed');}
    async function connectSub(sub){
        assertSub(sub);
        const generation=++sub.streamGeneration,g=sub.owned.grant;
        let first=true;
        const connection=await stream(sub.trust,token,{source:g.secret,body:{sessionId:g.sessionId}},value=>{
            if(sub.closed||generation!==sub.streamGeneration)return;
            try{assertSub(sub);validateOwnEvent(value,g,sub.last);
                if(!first&&sub.last>0&&value.seq!==sub.last+1){sub.lossBoundary={lastReceived:sub.last,lastAcknowledged:sub.ack,incoming:value.seq};closeSub(sub,'event-gap');return;}
                if(generation>1&&first&&(value.kind!=='snapshot'||!value.metadata))throw Error('Renewal requires authorized snapshot');
                const resynchronized=generation>1&&first;first=false;sub.last=value.seq;
                if(sub.pending.length>=8)throw Error('SSE backpressure');
                sub.pending.push({value,sent:false,resynchronized,generation,recoveryBoundary:resynchronized?{...sub.recoveryBoundary,snapshotSequence:value.seq,continuityUnknown:true}:null});sub.queuePeak=Math.max(sub.queuePeak,sub.pending.length);
                flushSub(sub);if(!sub.timer)deadlineSub(sub);
            }catch{closeSub(sub,'authority-revoked');}
        },()=>{if(generation===sub.streamGeneration)closeSub(sub,'engine-stream-closed')});
        if(sub.closed||generation!==sub.streamGeneration){connection.cancel();throw Error('Subscription ended');}
        sub.connection=connection;assertSub(sub);
        sub.renewTimer=setTimeout(()=>void renewSub(sub),55000);
    }
    async function renewSub(sub){
        try{assertSub(sub);if(!sub.active)throw Error('Inactive renewal');
            if(sub.pending.length||sub.ack!==sub.last){sub.renewPending=true;return;}
            sub.renewPending=false;
            // A planned, single attempt, never a reconnect following a failure.
            // Invalidate callbacks BEFORE closing the old connection.
            sub.recoveryBoundary={lastReceived:sub.last,lastAcknowledged:sub.ack};
            sub.streamGeneration++;sub.connection?.cancel();sub.connection=null;
            assertSub(sub);sub.renewals++;await connectSub(sub);
        }catch{closeSub(sub,'renewal-unconfirmed');}
    }
    async function scoped(scope,trust,owned,op,body){
        const g=owned.grant,owner=revision;
        const valid=()=>{check(scope,trust,owner);if(current!==owned)throw Error('Scoped response superseded');};
        const only=keys=>Object.keys(body).every(k=>keys.includes(k));
        if(op==='prewarm')return denied(); // no qualified queue/account authority
        if(op==='eventAck'||op==='unsubscribe'){
            if(!only(op==='eventAck'?['subscriptionId','seq']:['subscriptionId']))return denied();
            const sub=subscriptions.get(body.subscriptionId);
            if(!sub||sub.owned!==owned||sub.scope.record!==scope.record)return denied();
            valid();
            if(op==='unsubscribe'){closeSub(sub);return {status:200,body:{stage:'subscription-closed'}};}
            if(!Number.isSafeInteger(body.seq)||body.seq<0||body.seq>sub.last||body.seq<sub.ack)return denied();
            sub.ack=body.seq;sub.active=true;sub.pending=sub.pending.filter(item=>item.value.seq>body.seq);
            flushSub(sub);deadlineSub(sub);if(sub.renewPending&&sub.pending.length===0&&sub.ack===sub.last)void renewSub(sub);return {status:200,body:{stage:'event-acknowledged'}};
        }
        if(body.sessionId!==g.sessionId)return denied();
        if(op==='subscribe'){
            if(!only(['sessionId'])||!stream||subscriptions.size>=1)return denied();
            const sub={id:crypto.randomUUID(),scope,trust,owned,owner,epoch:g.epoch,pending:[],last:0,ack:0,active:false,closed:false,streamGeneration:0,renewals:0,queuePeak:0};
            subscriptions.set(sub.id,sub);deadlineSub(sub);
            try{await connectSub(sub);valid();return {status:200,body:{subscriptionId:sub.id,leaseMs:60000,renewAfterMs:55000,continuity:'authorized-snapshot'}};}
            catch{closeSub(sub);throw Error('Own subscription unavailable');}
        }
        const allowed={metadata:['sessionId'],setVolume:['sessionId','value'],library:['sessionId','page','limit'],artwork:['sessionId']}[op];
        if(!allowed||!only(allowed))return denied();
        const payload={operation:op==='setVolume'?'volume':op,sessionId:g.sessionId};
        let volumeRevision;
        if(op==='setVolume'){
            if(!Number.isSafeInteger(body.value)||body.value<0||body.value>200)return denied();
            volumeRevision=owned.volumeRevision=(owned.volumeRevision??0)+1;
            Object.assign(payload,{value:body.value,revision:volumeRevision});
        }
        if(op==='library'){
            if(!Number.isSafeInteger(body.page)||body.page<0||body.page>10||!Number.isSafeInteger(body.limit)||body.limit<1||body.limit>2)return denied();
            Object.assign(payload,{page:body.page,limit:body.limit});
        }
        const result=await send(trust,token,{operation:'source',source:g.secret,body:payload});valid();
        if(result.status!==200)return {status:result.status,body:{error:'Scoped operation rejected',...(op==='setVolume'?{mayHaveApplied:result.body?.mayHaveApplied!==false,backend:['native-unidentified','native-pulse','callback-memory','unavailable'].includes(result.body?.backend)?result.body.backend:'unconfirmed',confirmationStage:'unconfirmed',capabilityState:'output-unverified'}:{})}};
        if(op==='metadata')validateOwnMetadata(result.body,g);
        if(op==='library')validatePublicLibrary(result.body);
        if(op==='artwork')validateFixedArtwork(result.body);
        if(op==='setVolume'&&(volumeRevision!==owned.volumeRevision||result.body?.revision!==volumeRevision||result.body?.value!==body.value||result.body?.stage!=='app-volume-returned'||!Number.isInteger(result.body?.nativeValue)||result.body.nativeValue<0||result.body.nativeValue>200||!['native-unidentified','native-pulse','callback-memory','unavailable'].includes(result.body?.backend)))throw Error('Volume response superseded');
        if(op==='setVolume'){
            // Native return/getter confirmation is not a backend, sample or
            // audible-device witness. Do not echo stronger server assertions.
            if(result.body.backend==='callback-memory')return {status:503,body:{error:'Output volume unsupported',mayHaveApplied:true,backend:'callback-memory',confirmationStage:'unsupported-output',capabilityState:'unsupported'}};
            if(result.body.nativeValue!==body.value)return {status:503,body:{error:'Native volume readback mismatch',mayHaveApplied:true,backend:result.body.backend,confirmationStage:'unconfirmed',capabilityState:'output-unverified'}};
            result.body={stage:'app-volume-returned',capabilityState:'native-confirmed-output-unverified',confirmationStage:'native-getter-confirmed',mayHaveApplied:true,revision:volumeRevision,value:body.value,nativeValue:result.body.nativeValue,backend:result.body.backend,
                verification:{nativeSetterAccepted:true,nativeGetterConfirmed:true,backendIndependentlyConfirmed:false,outputSampleGainVerified:false,physicalDeviceAudibleVerified:false}};
            owned.volumeOutcome={...result.body};
        }
        return result;
    }
    const denied=()=>({status:403,body:{error:'source-authorization-unavailable',mayHaveApplied:false}});
    const sameTrust=(a,b)=>a.boot===b.boot&&a.pin===b.pin&&a.nonce===b.nonce;
    function check(scope,trust,owner) {
        guard.assertCurrent(scope);
        if(!sameTrust(trust,descriptor())||owner!==revision) throw Error('Source ownership revoked; rollback is not implied');
    }
    function enqueue(fn) {
        const work=lifecycle.then(fn);
        lifecycle=work.catch(()=>{});
        return work;
    }
    function requireAvailable(trust) {
        if(blockedBoot===trust.boot) throw Error('Source authority cleanup unconfirmed');
        if(blockedBoot!==null) blockedBoot=null; // a verified new Engine Boot is a new authority domain
    }
    function uncertain(trust) {
        blockedBoot=trust.boot;
        onUnconfirmedCleanup(trust);
    }
    async function retire(owned) {
        if(!owned) return;
        const {trust,grant:g}=owned;
        try {
            if(!sameTrust(trust,descriptor())) return; // old process/Boot cannot own the new Engine
            const r=await send(trust,token,{operation:'source',body:{operation:'release',sessionId:g.sessionId},source:g.secret});
            if(r.status!==200||r.body?.stage!=='authority-revoked') throw Error('Revocation unconfirmed');
        } catch {
            uncertain(trust);
            throw Error('Source cleanup unconfirmed; rollback is not implied');
        }
    }
    function revoke({conditional=false}={}) {
        const old=current;
        closeSubscriptions('authority-revoked');
        current=null;
        if(!conditional||approved===old?.approval) approved=null;
        revision++;
        // A pending factory response is drained and conditionally retired by its
        // own queue entry BEFORE this barrier, even when no grant is known yet.
        return enqueue(()=>retire(old));
    }
    guard.onRevoke?.(record=>{
        if(current?.scope.record===record||approved?.scope.record===record) void revoke().catch(()=>{});
    });
    function approve(event,intent) {
        const scope=guard.capture(event),trust=descriptor();
        requireAvailable(trust);
        if(!adapter||trust.factory!==adapter.kind||!adapter.accept(intent)) throw Error('Qualified fixed source adapter required');
        approved={scope,trust,intent};
    }
    function validGrant(g,s,scope,trust,owner,intent) {
        return g&&s&&g.principal===principalId&&g.mainGeneration===mainGeneration&&
            g.documentGeneration===scope.record.generation&&g.boot===trust.boot&&
            g.ownershipRevision===owner&&g.assetId===intent&&g.sessionId===s.sessionId&&
            g.assetId===s.assetId&&Number.isSafeInteger(g.sourceGeneration)&&g.sourceGeneration>0&&
            /^[a-f0-9]{64}$/.test(g.secret??'')&&/^[a-f0-9]{32}$/.test(g.leaseId??'');
    }
    async function execute(event,request) {
        const scope=guard.capture(event),trust=descriptor(),op=request.operation,body=request.body??{};
        requireAvailable(trust);
        if(!body||Object.getPrototypeOf(body)!==Object.prototype) return denied();
        if(op==='create'||op==='reopen') {
            const approval=approved;
            if(Object.keys(body).some(k=>k!=='assetId')||!approval||body.assetId!==approval.intent||
                approval.scope.record!==scope.record||!sameTrust(approval.trust,trust)) return denied();
            const intent=approval.intent,owner=++revision;
            closeSubscriptions('source-replaced');
            current=null;
            return enqueue(async()=>{
                requireAvailable(trust);
                check(scope,trust,owner);
                let result;
                try {
                    result=await send(trust,token,{operation:'source',issuer,body:{operation:op,intent,
                        boot:trust.boot,principal:principalId,mainGeneration,
                        documentGeneration:scope.record.generation,ownershipRevision:owner}});
                } catch {
                    // Timeout is not evidence that issuance was rolled back.
                    uncertain(trust);
                    throw Error('Source issuance unconfirmed');
                }
                if(result.status!==201) {check(scope,trust,owner);return result;}
                const g=result.body?.grant,s=result.body?.session;
                if(!validGrant(g,s,scope,trust,owner,intent)) {uncertain(trust);throw Error('Invalid source grant');}
                const owned={scope,trust,grant:g,sequence:0,approval};
                try {check(scope,trust,owner);} catch(error) {
                    // A late valid grant must not survive navigation/cancellation.
                    // Newer issuance is still queued behind this exact retirement.
                    await retire(owned);
                    throw error;
                }
                current=owned;
                // No source proof, lease or principal binding crosses IPC.
                return {status:200,body:{sessionId:s.sessionId,assetId:s.assetId,codec:s.codec,
                    durationMs:s.durationMs,sampleRate:s.sampleRate,bitDepth:s.bitDepth,
                    channelCount:s.channelCount,title:s.title,artistName:s.artistName,capabilities:s.capabilities}};
            });
        }
        const owned=current;
        if(!owned||owned.scope.record!==scope.record||!sameTrust(owned.trust,trust)) return denied();
        const g=owned.grant,owner=revision;
        if(['metadata','setVolume','library','artwork','subscribe','eventAck','unsubscribe','prewarm'].includes(op))return scoped(scope,trust,owned,op,body);
        if(op==='release') {
            if(Object.keys(body).some(k=>k!=='sessionId')||body.sessionId!==g.sessionId) return denied();
            await revoke({conditional:true});
            guard.assertCurrent(scope);
            if(!sameTrust(trust,descriptor())) throw Error('Engine changed after retirement');
            return {status:200,body:{stage:'authority-revoked',nativeRollback:false}};
        }
        let query;
        if(op==='time'||op==='status') {
            if(request.body!==undefined) return denied();
            query={operation:'source',body:{operation:'read',sessionId:g.sessionId},source:g.secret};
        } else if(op==='touch') {
            if(Object.keys(body).some(k=>k!=='sessionId')||body.sessionId!==g.sessionId) return denied();
            query={operation:'source',body:{operation:'touch',sessionId:g.sessionId},source:g.secret};
        } else if(op==='control') {
            if(Object.keys(body).some(k=>!['boot','epoch','sessionId'].includes(k))||body.boot!==g.boot||
                body.sessionId!==g.sessionId||g.epoch!=='') return denied();
            query={operation:'control',body,source:g.secret};
        } else {
            const allowed={load:['sessionId','assetId','startMs','command'],seek:['sessionId','posMs','command'],
                pause:['sessionId','command'],resume:['sessionId','command'],stop:['sessionId','command'],intent:['sessionId','command']}[op];
            const c=body.command;
            if(!allowed||Object.keys(body).some(k=>!allowed.includes(k))||body.sessionId!==g.sessionId||
                (body.assetId!==undefined&&body.assetId!==g.assetId)||!c||
                Object.keys(c).some(k=>!['boot','epoch','sessionId','loadGeneration','sequence','type','requestId'].includes(k))||
                c.boot!==g.boot||c.epoch!==g.epoch||c.sessionId!==g.sessionId||c.type!==op||
                !Number.isSafeInteger(c.sequence)||c.sequence<=owned.sequence||
                !Number.isSafeInteger(c.loadGeneration)||c.loadGeneration<0||
                typeof c.requestId!=='string'||c.requestId.length<1||c.requestId.length>128) return denied();
            for(const k of ['startMs','posMs']) if(body[k]!==undefined&&(!Number.isSafeInteger(body[k])||body[k]<0)) return denied();
            owned.sequence=c.sequence;
            query={operation:op,body,source:g.secret};
        }
        const issuedSequence=owned.sequence;
        const result=await send(trust,token,query);
        check(scope,trust,owner);
        if(current!==owned||issuedSequence!==owned.sequence) throw Error('Source response superseded');
        if(op==='control'&&result.status===200) {
            if(result.body?.boot!==g.boot||result.body?.sessionId!==g.sessionId||!/^[a-f0-9]{32}$/.test(result.body?.epoch??'')) throw Error('Unconfirmed control claim');
            g.epoch=result.body.epoch;
        }
        if(body.command&&result.status===200) {
            const ack={load:'media-committed',seek:'seek-dispatched',intent:'intent-invalidated',pause:'native-committed',resume:'native-committed',stop:'native-committed'}[op];
            if(result.body?.ack!==ack) throw Error('Unconfirmed command stage');
            if(Object.keys(body.command).some(k=>result.body?.command?.[k]!==body.command[k])) throw Error('Unconfirmed command identity');
            g.commandGeneration=body.command.loadGeneration;
        }
        if(op==='stop'&&result.status===200) {
            const stoppedRevision=revision+1;
            await revoke({conditional:true});
            guard.assertCurrent(scope);
            if(stoppedRevision!==revision||!sameTrust(trust,descriptor())) throw Error('Stop response superseded');
        }
        return result;
    }
    function invalidateEngine(nonce, reason) {
        if(current?.trust.nonce!==nonce&&approved?.trust.nonce!==nonce)return;
        closeSubscriptions(reason);current=null;approved=null;revision++;
    }
    return {approve,execute,revoke,volumeDiagnostics:()=>current?.volumeOutcome?{...current.volumeOutcome}:null,subscriptionDiagnostics:()=>[...subscriptions.values()].map(s=>({id:s.id,sessionId:s.owned.grant.sessionId,streamGeneration:s.streamGeneration,renewals:s.renewals,renewPending:s.renewPending===true,queuePeak:s.queuePeak,pending:s.pending.length,last:s.last,ack:s.ack})),engineRetiring(nonce){invalidateEngine(nonce,'engine-retiring')},engineExited(nonce){
        // Owned process exit is separate from intent-time revocation. Neither
        // callback sends old proof material to a replacement Engine.
        invalidateEngine(nonce,'engine-exited');
    },bindFactory(value){
        if(adapter||!value||typeof value.accept!=='function'||typeof value.kind!=='string') throw Error('Invalid Main factory adapter');
        adapter=Object.freeze({...value});
    },owns:()=>current!==null};
}
