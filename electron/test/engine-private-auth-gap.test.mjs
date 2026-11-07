import {test} from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import os from 'node:os';import path from 'node:path';import crypto from 'node:crypto';
import {createEngineAccountHost} from '../engine-account-context.mjs';
test('Main auth intent must be transmitted to its Engine',async t=>{
 let host; const parent=fs.mkdtempSync(path.join(os.tmpdir(),'u219-gap-'));fs.chmodSync(parent,0o700);t.after(()=>{current=null;host?.exited(owner);host?.close();fs.rmSync(parent,{recursive:true,force:true})});let current;const owner=Object.freeze({id:crypto.randomUUID()});const life={isOwned:o=>o===owner,isCurrent:o=>o===current,currentOwner:()=>current,suspend:async()=>{current=null}};let calls=0;
 host=createEngineAccountHost({parent,getLifecycle:()=>life,getTrustedBoot:()=>({boot:'a'.repeat(32),nonce:host.bootNonce(host.current()),pid:123}),sendAuthentication:async q=>{calls++;return{status:202,body:{authBinding:q.binding,state:{authentication:'accepted'},nativeAuthenticated:false}}}});const ctx=host.prepare(owner);current=owner;host.owned(owner);
 await (host.requestAuthentication?host.requestAuthentication(ctx):host.beginAuthentication(ctx));assert.equal(calls,1,'a local attempt alone never reaches the Engine');assert.equal(ctx.nativeAuthenticated,false)
});
