import {test} from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import vm from 'node:vm';import path from 'node:path';import {pathToFileURL} from 'node:url';import {EventEmitter} from 'node:events';import {createCredentialInputHost} from '../credential-input-host.mjs';
function rig(enabled=true){const handlers=new Map(),calls=[];let win,active=true;const abort=new AbortController();const context={},attempt={},state={phase:'accepted',binding:attempt};class Window extends EventEmitter{constructor(opts){super();this.opts=opts;win=this;const wc=this.webContents=new EventEmitter();wc.mainFrame={url:''};wc.isDestroyed=()=>!!this.dead;wc.getURL=()=>wc.mainFrame.url;wc.session=new EventEmitter();wc.session.setPermissionRequestHandler=fn=>wc.permission=fn;wc.session.webRequest={onBeforeRequest:fn=>wc.request=fn};wc.setWindowOpenHandler=fn=>wc.open=fn}isDestroyed(){return !!this.dead}destroy(){this.dead=true;this.emit('closed')}show(){}async loadFile(file){this.webContents.mainFrame.url=pathToFileURL(file).href;queueMicrotask(()=>this.webContents.emit('did-finish-load'))}}
const accountHost={run:async(_c,fn)=>fn({signal:abort.signal}),assertCurrent:c=>{if(c!==context||!active)throw Error()},current:()=>context,authStatus:()=>state,cancel:async()=>{calls.push('cancel')},handoffCredential:async(c,a,v)=>{if(c!==context||a!==attempt)throw Error();calls.push('credentials');return {ok:true}},handoffChallenge:async(c,a,id,reply)=>{if(c!==context||a!==attempt)throw Error();calls.push('challenge');return {ok:true}},pollAuthentication:async()=>({...state,inputChallenge:null})};const host=createCredentialInputHost({BrowserWindow:Window,handle:(k,v)=>handlers.set(k,v),accountHost,enabled});return {host,context,attempt,state,calls,handlers,get win(){return win},get event(){return {sender:win.webContents,senderFrame:win.webContents.mainFrame}},revoke:()=>{active=false;abort.abort()}}}
const value=()=>({email:'u220-user@example.invalid',password:'u220-synthetic-sentinel'});
async function opened(){const r=rig();r.host.open(r.context,r.attempt);await Promise.resolve();return r}
test('production input opener stays closed',()=>{const r=rig(false);assert.throws(()=>r.host.open(r.context,r.attempt));assert.equal(r.win,undefined)});
test('owned local main frame can submit only fixed credential shape',async()=>{const r=await opened(),v=value();assert.equal((await r.handlers.get('private-input:credentials')(r.event,v)).ok,true);assert.equal(v.password,'');assert.deepEqual(r.calls,['credentials']);r.host.close()});
for(const which of ['other-window','subframe','remote-url','revoked','foreign-scope'])test('reject '+which+' input without delivery',async()=>{const r=await opened();let e=r.event;if(which==='other-window')e={...e,sender:{}};if(which==='subframe')e={...e,senderFrame:{url:e.senderFrame.url}};if(which==='remote-url')e.senderFrame.url='https://music.apple.com';if(which==='revoked')r.revoke();if(which==='foreign-scope')r.state.binding={};assert.equal((await r.handlers.get('private-input:credentials')(e,value())).ok,false);assert(!r.calls.includes('credentials'));r.host.close()});
test('navigation invalidates old document before input',async()=>{const r=await opened();const e=r.event;r.win.webContents.emit('will-navigate',{preventDefault(){}});assert.equal((await r.handlers.get('private-input:credentials')(e,value())).ok,false);assert(r.calls.includes('cancel'))});
test('Renderer cannot supply owner or epoch',async()=>{const r=await opened();assert.equal((await r.handlers.get('private-input:credentials')(r.event,{...value(),engineOwnerId:'foreign'})).ok,false);assert(!r.calls.includes('credentials'));r.host.close()});
test('window keeps sandbox isolation ephemeral partition and no network',async()=>{const r=await opened();const prefs=r.win.opts.webPreferences;assert.equal(prefs.sandbox,true);assert.equal(prefs.contextIsolation,true);assert.equal(prefs.nodeIntegration,false);assert.equal(prefs.webSecurity,true);assert(!prefs.partition.startsWith('persist:'));let result;r.win.webContents.request({url:'https://example.invalid'},v=>result=v);assert.equal(result.cancel,true);assert.equal(r.win.webContents.open().action,'deny');r.host.close()});
test('local preload never exposes private input to remote content or subframes',()=>{const source=fs.readFileSync(new URL('../secure-credential-preload.cjs',import.meta.url),'utf8');for(const [protocol,frame]of [['https:',true],['file:',false],['file:',true]]){let exposed;vm.runInNewContext(source,{require:()=>({contextBridge:{exposeInMainWorld:(name,api)=>{exposed={name,api}}},ipcRenderer:{invoke:async()=>({ok:true})}}),process:{isMainFrame:frame},location:{protocol,pathname:'/app/secure-credential.html'}});assert.equal(!!exposed,protocol==='file:'&&frame);if(exposed)assert.deepEqual(Object.keys(exposed.api).sort(),['cancel','challenge','credentials','view'])}});
test('Main production installation cannot be enabled by Renderer flag',()=>{const source=fs.readFileSync(new URL('../main.mjs',import.meta.url),'utf8');assert(source.includes('accountHost:engineAccountHost,enabled:false'));const preload=fs.readFileSync(new URL('../preload.cjs',import.meta.url),'utf8');assert(!preload.includes('private-input:'))});

for(const modern of [true,false])test('document-start revokes '+(modern?'Electron details':'positional navigation')+' before same-URL reload',async()=>{const r=await opened(),e=r.event;const wc=r.win.webContents;if(modern)wc.emit('did-start-navigation',{url:wc.getURL(),isMainFrame:true,isSameDocument:false});else wc.emit('did-start-navigation',{},wc.getURL(),false,true);assert.equal((await r.handlers.get('private-input:credentials')(e,value())).ok,false);assert(r.calls.includes('cancel'))});

// U2-21F: product handler regressions; EventEmitter fixtures are not Electron GUI evidence.
const cspLoadFailure=(wc,scope)=>wc.emit('did-fail-load',{},-30,'ERR_BLOCKED_BY_CSP',wc.getURL(),scope,11,22);
test('subframe CSP load failure preserves the trusted window and current attempt',async()=>{
 const r=await opened(),wc=r.win.webContents,attempt=r.state.binding;
 cspLoadFailure(wc,false);
 assert.equal(r.win.isDestroyed(),false,'CSP must block the child without retiring the healthy parent');
 assert.deepEqual(r.calls,[],'child failure must not call account cancellation');
 assert.equal(r.state.binding,attempt);assert.equal(r.state.phase,'accepted');
 assert.equal((await r.handlers.get('private-input:credentials')(r.event,value())).ok,true);
 assert.deepEqual(r.calls,['credentials']);r.host.close();
});
test('subframe failure preserves all original IPC ownership and shape guards',async()=>{
 const r=await opened();cspLoadFailure(r.win.webContents,false);
 const events=[{...r.event,sender:{}},{...r.event,senderFrame:{url:r.event.senderFrame.url}}];
 for(const e of events)assert.equal((await r.handlers.get('private-input:credentials')(e,value())).ok,false);
 assert.equal((await r.handlers.get('private-input:credentials')(r.event,{...value(),engineOwnerId:'foreign'})).ok,false);
 assert.deepEqual(r.calls,[],'neither rejected input nor child failure may reach/cancel the attempt');
 assert.equal((await r.handlers.get('private-input:view')(r.event)).nativeAuthenticated,false);
 r.host.close();
});
for(const frameScope of [true,undefined,null,0,1,'false','unknown'])test('main or unknown load failure retires and blocks IPC: '+String(frameScope),async()=>{
 const r=await opened(),oldEvent=r.event;cspLoadFailure(r.win.webContents,frameScope);
 assert.equal(r.win.isDestroyed(),true);assert.deepEqual(r.calls,['cancel']);
 assert.equal((await r.handlers.get('private-input:credentials')(oldEvent,value())).ok,false);
 assert.deepEqual(r.calls,['cancel'],'retired Main must never deliver input');
});
for(const name of ['render-process-gone','destroyed'])test(name+' remains terminal after a child CSP rejection',async()=>{
 const r=await opened(),oldEvent=r.event;cspLoadFailure(r.win.webContents,false);r.win.webContents.emit(name);
 assert.equal(r.win.isDestroyed(),true);assert.deepEqual(r.calls,['cancel']);
 assert.equal((await r.handlers.get('private-input:credentials')(oldEvent,value())).ok,false);
 assert.deepEqual(r.calls,['cancel']);
});
for(const modern of [true,false])test('child navigation then CSP rejection leaves Main bound: '+(modern?'details':'positional'),async()=>{
 const r=await opened(),wc=r.win.webContents;
 if(modern)wc.emit('did-start-navigation',{url:wc.getURL(),isMainFrame:false,isSameDocument:false});else wc.emit('did-start-navigation',{},wc.getURL(),false,false);
 cspLoadFailure(wc,false);assert.equal(r.win.isDestroyed(),false);assert.deepEqual(r.calls,[]);r.host.close();
});
test('Main navigation still retires after child failure',async()=>{
 const r=await opened();cspLoadFailure(r.win.webContents,false);let prevented=false;
 r.win.webContents.emit('will-navigate',{preventDefault(){prevented=true}});
 assert(prevented);assert.equal(r.win.isDestroyed(),true);assert.deepEqual(r.calls,['cancel']);
});
test('Account AbortSignal still retires after child failure',async()=>{
 const r=await opened();cspLoadFailure(r.win.webContents,false);r.revoke();
 assert.equal(r.win.isDestroyed(),true);assert.deepEqual(r.calls,['cancel']);
});
