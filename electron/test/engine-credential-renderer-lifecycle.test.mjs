import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
// Renderer scheduling units only. Never counts as actual Chromium/IPC E2E.
function rig(){let tick,options;const pending=[],snapshots=[],listeners=new Map();let clears=0,closed=0;const api={view:()=>new Promise(resolve=>pending.push(resolve)),credentials:async()=>({ok:true}),challenge:async()=>({ok:true}),cancel:async()=>({ok:true})};const sheet={openSignIn(){},onSnapshot:s=>snapshots.push(s),close(){closed++}};const source=fs.readFileSync(new URL('../secure-credential.js',import.meta.url),'utf8').replace(/^import[^\n]*\n/,'');vm.runInNewContext(source,{window:{adamPrivateInput:api,addEventListener:(k,fn)=>listeners.set(k,fn)},createAccountSheet:v=>{options=v;return sheet},setInterval:fn=>{tick=fn;return 1},clearInterval:()=>clears++});return {tick:()=>tick(),options,pending,snapshots,listeners,get clears(){return clears},get closed(){return closed}}}
const reply=()=>({ok:true,phase:'challenge_required',challenge:{id:'a'.repeat(32)}});
test('renderer admits only one unresolved status poll',async()=>{const r=rig(),p=r.tick(),q=r.tick();const n=r.pending.length;for(const resolve of r.pending)resolve(reply());await Promise.all([p,q]);assert.equal(n,1,'overlapping polls can race challenge display')});
test('status captured before credential mutation cannot replace the new view',async()=>{const r=rig(),p=r.tick();await r.options.signIn('synthetic','synthetic');r.pending[0](reply());await p;assert.equal(r.snapshots.length,0,'pre-input status must be discarded')});
test('pagehide discards late status and cannot reopen the sheet',async()=>{const r=rig(),p=r.tick();r.listeners.get('pagehide')();r.pending[0](reply());await p;assert.equal(r.snapshots.length,0,'closed document cannot receive challenge view');assert(r.clears>0)});
test('cancel invalidates pending view immediately',async()=>{const r=rig(),p=r.tick();await r.options.onCancel();r.pending[0](reply());await p;assert.equal(r.snapshots.length,0,'cancelled UI cannot receive pending status');assert(r.clears>0)});
