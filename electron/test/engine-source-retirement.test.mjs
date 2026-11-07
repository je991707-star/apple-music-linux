import {test} from 'node:test';import assert from 'node:assert/strict';import {EventEmitter} from 'node:events';
import {createMainDocumentGuard} from '../playback-main-guard.mjs';
const {createMainSourceHost}=await import(process.env.U215_SOURCE_MODULE || '../playback-main-source.mjs');
function rig(options={}){const wc=new EventEmitter();Object.assign(wc,{mainFrame:{url:'https://music.apple.com/us'},getURL:()=>wc.mainFrame.url,isDestroyed:()=>false,isLoadingMainFrame:()=>false});const win={webContents:wc,isDestroyed:()=>false},guard=createMainDocumentGuard(()=>win);guard.attach(wc);wc.emit('did-finish-load');const event={sender:wc,senderFrame:wc.mainFrame};let trust={boot:'b',pin:'p',nonce:'n',factory:'private-fixed-test'},sent=[];let hold;
 const host=createMainSourceHost({guard,descriptor:()=>trust,token:'transport-only',issuer:'issuer-main-only',principalId:'main-only-principal',send:async(t,token,q)=>{sent.push(q);if(hold)return hold(q);if(q.operation==='source'&&q.body.operation==='create')return {status:201,body:{grant:{principal:q.body.principal,mainGeneration:q.body.mainGeneration,documentGeneration:q.body.documentGeneration,boot:t.boot,ownershipRevision:q.body.ownershipRevision,assetId:q.body.intent,sessionId:'engine-session',sourceGeneration:1,leaseId:'a'.repeat(32),secret:'c'.repeat(64),epoch:''},session:{sessionId:'engine-session',assetId:q.body.intent,codec:'alac',durationMs:4000}}};if(q.operation==='control')return{status:200,body:{boot:t.boot,epoch:'e'.repeat(32),sessionId:'engine-session'}};return{status:200,body:q.body.command?{command:q.body.command,ack:'native-committed'}:{stage:'authority-revoked'}};},...options});
 return{host,event,wc,sent,boot:()=>trust={...trust,boot:'new',nonce:'new-nonce'},hold:fn=>hold=fn};
}
async function acquire(r){r.host.bindFactory({kind:'private-fixed-test',accept:v=>v==='fixed-a'});r.host.approve(r.event,'fixed-a');return r.host.execute(r.event,{operation:'create',body:{assetId:'fixed-a'}})}
function subscriptionRig(){let emit,end,cancelled=0;const deliveries=[];const r=rig({stream:async(_t,_token,_q,onEvent,onEnd)=>{emit=onEvent;end=onEnd;return{cancel(){cancelled++;end()}}},deliver:(_scope,data)=>deliveries.push(data)});return {...r,deliveries,emit:v=>emit(v),cancelled:()=>cancelled}}
const ownFrame=(seq,extra={})=>({seq,kind:'command-pause',identity:{boot:'b',epoch:'',sessionId:'engine-session',assetId:'fixed-a',sourceGeneration:1,leaseId:'a'.repeat(32),ownershipRevision:1,documentGeneration:1,loadGeneration:0,commandSequence:0},metadata:null,...extra});
async function subscribe(r){await acquire(r);const v=await r.host.execute(r.event,{operation:'subscribe',body:{sessionId:'engine-session'}});assert.equal(v.status,200);return v.body.subscriptionId}

test('retirement intent immediately revokes Session/Lease and SSE before process exit',async()=>{
 const r=subscriptionRig();const id=await subscribe(r);r.host.engineRetiring('n');
 assert.equal(r.host.owns(),false);assert.equal(r.cancelled(),1);assert.equal(r.host.subscriptionDiagnostics().length,0);
 r.emit(ownFrame(1));assert.equal(r.deliveries.filter(v=>v.event).length,0);
 assert.equal((await r.host.execute(r.event,{operation:'touch',body:{sessionId:'engine-session'}})).status,403);
 assert.equal((await r.host.execute(r.event,{operation:'eventAck',body:{subscriptionId:id,seq:0}})).status,403);
});
test('late old Boot retirement cannot revoke the new Boot source',async()=>{
 const r=rig();await acquire(r);r.host.engineRetiring('n');r.boot();r.host.approve(r.event,'fixed-a');assert.equal((await r.host.execute(r.event,{operation:'create',body:{assetId:'fixed-a'}})).status,200);
 r.host.engineRetiring('n');r.host.engineExited('n');assert.equal(r.host.owns(),true);await r.host.revoke();
});
test('in-flight old response after retirement cannot restore source authority',async()=>{
 const r=rig();await acquire(r);let done;r.hold(()=>new Promise(resolve=>done=resolve));
 const pending=r.host.execute(r.event,{operation:'touch',body:{sessionId:'engine-session'}});
 r.host.engineRetiring('n');done({status:200,body:{stage:'engine-accepted'}});await assert.rejects(pending,/revoked|superseded/);assert.equal(r.host.owns(),false);
});
test('retirement removes prior Main approval, not just the active source',async()=>{
 const r=rig();r.host.bindFactory({kind:'private-fixed-test',accept:v=>v==='fixed-a'});r.host.approve(r.event,'fixed-a');r.host.engineRetiring('n');
 assert.equal((await r.host.execute(r.event,{operation:'create',body:{assetId:'fixed-a'}})).status,403);
});
