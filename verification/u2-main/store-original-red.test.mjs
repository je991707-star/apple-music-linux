import {test} from 'node:test';import assert from 'node:assert/strict';import fs from 'node:fs';import vm from 'node:vm';
const source=fs.readFileSync(process.env.U2_MAIN_BASELINE,'utf8');
const body=source.match(/ipcMain.handle\('store:read',\s*async \([^]*?\n\}\);/)[0];
for(const key of ['drm_password','MUSIC_TOKEN','session_token','auth_cookie','device_secret','refresh_token','library.key'])test('Renderer cannot read synthetic '+key,async()=>{
 let read;const ctx={ipcMain:{handle:(_,fn)=>read=fn},_storeKeyRe:/^[a-zA-Z][a-zA-Z0-9_:.-]{0,127}$/,_storeLoad:async()=>({[key]:'offline-synthetic-secret'})};vm.runInNewContext(body,ctx);assert.equal(await read({},key),null);
});
