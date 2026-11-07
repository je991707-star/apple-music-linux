// Whole normal Main/preload/Controller, empty private profile, no source factory.
import {app,BrowserWindow,session} from 'electron/main';import fs from 'node:fs';
let win,done=false;const rows=[];const ordered=process.env.ADAM_VLC_ORDERED==='1';
const delay=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,ms=50000){const end=Date.now()+ms;while(Date.now()<end){try{const value=await fn();if(value)return value}catch{}await delay(100)}throw Error('bounded deadline')}
const ipc=code=>win.webContents.executeJavaScript(code);
async function test(name,fn){try{rows.push({name,status:'PASS',evidence:await fn()})}catch(e){rows.push({name,status:'FAIL',error:e.message})}}
app.setPath('userData','/private/home/.config/adam/electron-session');app.setPath('sessionData','/private/home/.config/adam/electron-session');app.setPath('crashDumps','/private/crash');app.setAppLogsPath('/private/logs');
app.on('browser-window-created',(_,w)=>{if(!win)win=w});
app.whenReady().then(()=>session.fromPartition('persist:apple-music').webRequest.onBeforeRequest((d,cb)=>{let cancel=true;try{const h=new URL(d.url).hostname;cancel=h==='localhost'||h==='::1'||/^127\./.test(h)||/^(idmsa|appleid|account)\.apple\.com$/.test(h)}catch{}cb({cancel})}));
await import('./main.mjs');
async function finish(){if(done)return;done=true;clearTimeout(watchdog);fs.writeFileSync('/private/logs/results.json',JSON.stringify({mode:ordered?'ordered':'legacy',rows},null,2));win?.destroy();app.quit()}
const watchdog=setTimeout(()=>void finish(),95000);
void app.whenReady().then(async()=>{
 await test('private-paths-and-bus',async()=>{if(fs.existsSync('/var/home/h97m')||!process.env.DBUS_SESSION_BUS_ADDRESS?.includes('/private/run/bus'))throw Error('private boundary');return {private:true}});
 await test('normal-anonymous-Apple-page',async()=>{await until(()=>win,5000);await until(()=>ipc(`location.origin==='https://music.apple.com'&&document.readyState==='complete'&&!!window.adamBridge&&!!window.MusicKit`));const state=await ipc(`({authorized:MusicKit.getInstance().isAuthorized,origin:location.origin})`);if(state.authorized)throw Error('not anonymous');win.show();fs.writeFileSync('/private/logs/normal-ui.png',(await win.webContents.capturePage()).toPNG());return state});
 await test('real-Chromium-sandbox-and-context-isolation',async()=>{const v=await ipc(`({isolation:adamBridge.playbackIsolation,node:typeof require})`);if(!v.isolation.sandboxed||!v.isolation.contextIsolated||v.node!=='undefined')throw Error('isolation');return v});
 await test('normal-non-sensitive-preferences',async()=>{const value=await ipc(`(async()=>{await adamBridge.storeWrite('resumeEnabled',false);return await adamBridge.storeRead('resumeEnabled')})()`);if(value!==false)throw Error('preference');return {readWrite:true}});
 await test('legacy-ordered-opt-in-boundary',async()=>{const flag=await ipc('adamBridge.orderedPlaybackEnabled');if(flag!==ordered)throw Error('flag');const value=await ipc(`adamBridge.orderedPlayback({operation:'time'}).catch(()=>({disabled:true}))`);if(ordered?(value.status!==200||value.body.state!=='unowned'):value.disabled!==true)throw Error('opt-in principal');return {ordered,value}});
 await test('credential-shaped-store-key-denied',async()=>{const denied=await ipc(`Promise.all(['drm_password','MUSIC_TOKEN','auth_cookie','library.key'].map(k=>adamBridge.storeRead(k).then(()=>false,()=>true)))`);if(denied.some(v=>!v))throw Error('store permission');return {denied:denied.length}});
 await test('whole-normal-controller-seek-interceptor',async()=>{await until(()=>ipc(`String(MusicKit.getInstance().seekToTime).includes('debounce-reset')`),15000);return {normalController:true}});
 await test('renderer-environment-no-Main-authority',async()=>{const bytes=fs.readFileSync('/proc/'+win.webContents.getOSProcessId()+'/environ').toString();if(/ADAM_API_TOKEN=|ADAM_MAIN_SOURCE_ISSUER=|ADAM_MAIN_BOOT_NONCE=/.test(bytes))throw Error('authority inherited');return {noToken:true}});
 await finish();
}).catch(e=>{rows.push({name:'observer-error',status:'FAIL',error:e.message});void finish()});
