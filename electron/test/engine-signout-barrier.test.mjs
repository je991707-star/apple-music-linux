import {test} from 'node:test';import assert from 'node:assert/strict';
const {clearCredentialsFlow}=await import(process.env.U215_SIGNOUT_MODULE || '../drm-signout.mjs');
test('signout completion must await the owned process replacement barrier',async()=>{
 let ack,settled=false;const flow=clearCredentialsFlow({deleteCredentials:async()=>true,reloadEngine:()=>new Promise(resolve=>ack=resolve)}).then(v=>{settled=true;return v});
 await new Promise(resolve=>setImmediate(resolve));assert.equal(settled,false);ack();assert.deepEqual(await flow,{ok:true});
});
test('unconfirmed replacement cannot publish signout completion',async()=>{
 const observed=await clearCredentialsFlow({deleteCredentials:async()=>true,reloadEngine:()=>Promise.reject(Error('synthetic exit unconfirmed'))}).catch(()=>({ok:false}));
 assert.equal(observed.ok,false);
});
