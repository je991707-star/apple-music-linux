import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {createWebAccountContext} from './web-account-context.mjs';
import {createWebLoginDiagnostics} from './web-login-diagnostics.mjs';

const appleHosts=new Set(['music.apple.com','authorize.music.apple.com','appleid.apple.com','idmsa.apple.com','account.apple.com','auth.music.apple.com']);
// Electron43 exposes event details; retain old positional callbacks explicitly.
export function webNavigationDetails(event,url,isInPlace,isMainFrame){
    if(typeof event?.url==='string')return {url:event.url,isSameDocument:event.isSameDocument===true,isMainFrame:event.isMainFrame===true};
    return {url:typeof url==='string'?url:null,isSameDocument:isInPlace===true,isMainFrame:isMainFrame===true};
}
export function webLoginNavigation(value){try{const u=new URL(value);return u.protocol==='https:'&&!u.username&&!u.password&&!u.port&&appleHosts.has(u.hostname);}catch{return false;}}
export function webLoginRequestAllowed(details){
    try{const u=new URL(details.url);if(!['https:','wss:'].includes(u.protocol)||u.username||u.password||u.port)return false;
        if(details.resourceType==='media'||/license|fairplay|\/stream|\.m3u8|\.m4[av]|\.mp[34]|\.aac|\.m4s/i.test(u.pathname))return false;
        if(u.hostname==='api.music.apple.com')return details.method==='GET'&&!/\/play|\/stream|\/asset/i.test(u.pathname);
        return appleHosts.has(u.hostname)||u.hostname==='js-cdn.music.apple.com'||u.hostname.endsWith('.mzstatic.com')||u.hostname==='appleid.cdn-apple.com';
    }catch{return false;}
}
// Classifies public source-known roles WITHOUT recording paths or queries.
// A known role is not an allow rule or proof it is needed for authorization.
export function classifyWebLoginRequest(details,pending=false){
    const base={method:details.method,resourceType:details.resourceType,requestClass:pending?'AUTH_WINDOW_UNREVIEWED':'PAGE_UNREVIEWED'};
    try{const u=new URL(details.url);
        if(u.hostname==='www.apple.com'&&u.pathname.startsWith('/wss/fonts'))base.requestClass='FONT_ASSET';
        if(u.hostname==='xp.apple.com'&&u.pathname==='/register')base.requestClass='CAMPAIGN_REGISTRATION';
        if(u.hostname==='amp-api-edge.media.apple.com'&&/^\/v1\/engagement\/[a-z]{2}\/upsell$/.test(u.pathname))base.requestClass='MARKETING_UPSELL';
    }catch{}return base;
}
export function validatePrivateWebLogin(env=process.env,argv=process.argv){
    if(env.ADAM_WEB_LOGIN_ONLY!=='1')return false;
    const expected={HOME:'/private/home',XDG_CONFIG_HOME:'/private/config',XDG_DATA_HOME:'/private/data',XDG_CACHE_HOME:'/private/cache',XDG_RUNTIME_DIR:'/private/run'};
    if(process.platform!=='linux'||Object.entries(expected).some(([k,v])=>env[k]!==v)||!env.DBUS_SESSION_BUS_ADDRESS?.startsWith('unix:path=/private/run/bus')||env.DBUS_SYSTEM_BUS_ADDRESS!=='unix:path=/private/run/no-system-bus')throw Error('Private Web login environment required');
    if(argv.some(v=>/^(--no-sandbox|--password-store|--remote-debugging|--inspect|--enable-logging|--log-net-log)/.test(v))||env.APPIMAGE||env.ADAM_DEVTOOLS||env.ADAM_DEBUG||env.ADAM_VLC_ORDERED==='1'||env.ADAM_ENGINE_BIN)throw Error('Private Web login configuration refused');
    if(fs.existsSync('/var/home/h97m')||process.execPath!=='/app/adam')throw Error('Private filesystem required');
    const mounts=fs.readFileSync('/proc/self/mountinfo','utf8').split('\n');
    if(!mounts.some(v=>v.split(' ')[4]==='/private'&&v.includes(' - tmpfs '))||!mounts.some(v=>v.split(' ')[4]==='/app'&&v.split(' ')[5].split(',').includes('ro')))throw Error('Ephemeral private filesystem required');
    if(mounts.some(v=>v.split(' ')[4]?.startsWith('/private/')))throw Error('Nested profile mounts refused');
    for(const dir of [...Object.values(expected),'/private/session','/private/crash']) {
        const st=fs.lstatSync(dir);
        if(st.isSymbolicLink()||!st.isDirectory()||fs.realpathSync(dir)!==dir||st.uid!==process.getuid()||(st.mode&0o777)!==0o700)throw Error('Private directory ownership required');
    }
    const marker=JSON.parse(fs.readFileSync('/app/web-login-private.json','utf8'));if(marker.version!==1||marker.webOnly!==true)throw Error('Private staging required');
    return true;
}

// Install before ANY normal Main IPC registration. No legacy store, engine,
// credential, account or filesystem operation is permitted in this mode.
export function fenceWebLoginIPC(ipcMain,getWindow,onInvalidate){
    const handle=ipcMain.handle.bind(ipcMain),on=ipcMain.on.bind(ipcMain);
    ipcMain.handle=()=>{};ipcMain.on=()=>{};ipcMain.once=()=>{};
    const trusted=e=>{try{const w=getWindow();return w&&!w.isDestroyed()&&!w.webContents.isDestroyed()&&!w.webContents.isLoadingMainFrame()&&e.sender===w.webContents&&e.senderFrame===w.webContents.mainFrame&&new URL(w.webContents.getURL()).origin==='https://music.apple.com';}catch{return false;}};
    // Revocation can only remove authority; it can never establish an account.
    on('web-login:invalidate',(e,...args)=>{if(trusted(e)&&args.length===0)onInvalidate();});
    handle('web-login:status',e=>{if(!trusted(e))throw Error('Web login unavailable');return {scope:'web-only',engine:false,credentials:false};});
}

const sdkRead=`(()=>{try{const m=MusicKit.getInstance();return {musicUserToken:m.musicUserToken,developerToken:m.developerToken}}catch{return {}}})()`;
// One prerequisite contract for anonymous qualification AND explicit authorization.
// Never configures the first-party SDK or inspects its private initialization data.
export function inspectWebSDK(sdk){
    if(typeof sdk?.getInstance!=='function')return {reason:'SDK_NOT_READY'};
    let instance;try{instance=sdk.getInstance()}catch{return {reason:'SDK_GET_INSTANCE_THREW'}};
    if(instance==null)return {reason:'SDK_INSTANCE_NULL'};
    try{
        if(typeof instance.authorize!=='function')return {reason:'SDK_AUTHORIZE_MISSING'};
        if(typeof instance.addEventListener!=='function')return {reason:'SDK_EVENT_LISTENER_MISSING'};
        if(typeof instance.unauthorize!=='function')return {reason:'SDK_UNAUTHORIZE_MISSING'};
    }catch{return {reason:'SDK_INSTANCE_UNAVAILABLE'}};
    return {instance};
}
const sdkQualification=`(()=>{
    const inspected=(${inspectWebSDK.toString()})(globalThis.MusicKit);
    if(inspected.reason)return {reason:inspected.reason};
    window.__adamWebQualifiedInstance=inspected.instance;
    return {reason:'SDK_INSTANCE_READY'};
})()`;
const sdkAuthorize=`(async()=>{
    const inspected=(${inspectWebSDK.toString()})(globalThis.MusicKit);
    if(inspected.reason)return {reason:inspected.reason};
    const m=inspected.instance;
    if(window.__adamWebQualifiedInstance&&window.__adamWebQualifiedInstance!==m)return {reason:'SDK_INSTANCE_CHANGED'};
    try{if(window.__adamWebChangeWatch!==m){m.addEventListener('authorizationStatusDidChange',()=>window.adamWebLogin?.invalidate());window.__adamWebChangeWatch=m}}catch{return {reason:'SDK_EVENT_LISTENER_FAILED'}};
    try{await m.authorize()}catch{return {reason:'SDK_AUTHORIZE_REJECTED'}};
    try{if(globalThis.MusicKit.getInstance()!==m)return {reason:'SDK_INSTANCE_CHANGED'};return {musicUserToken:m.musicUserToken,developerToken:m.developerToken}}catch{return {reason:'AUTH_ATTEMPT_UNCONFIRMED'}}
})()`;

export function startPrivateWebLogin({app,BrowserWindow,session,Menu,dialog,preload,authPreload,onWindow=()=>{},probe=false,diagnostic}){
    const diag=diagnostic??createWebLoginDiagnostics({enabled:process.env.ADAM_WEB_LOGIN_DIAGNOSTICS==='1',write:row=>fs.appendFileSync('/diagnostics/reasons.jsonl',JSON.stringify(row)+'\n',{mode:0o600})});
    const partition='adam-private-web-'+crypto.randomUUID();
    const ses=session.fromPartition(partition,{cache:false});
    if(ses.isPersistent())throw Error('Memory session required');
    let closing=false,win,ending=null,consentPending=false,popupRequests=0,popupCreations=0;const children=new Set();
    const context=createWebAccountContext({
        authorizeSDK:async()=>{if(probe)throw Error('Probe cannot authorize');
            let pair;try{pair=await win.webContents.executeJavaScript(sdkAuthorize,true);}catch{diag('SDK_AUTHORIZE_REJECTED');throw Error('SDK authorization rejected');}
            if(['SDK_NOT_READY','SDK_INSTANCE_UNAVAILABLE','SDK_GET_INSTANCE_THREW','SDK_INSTANCE_NULL','SDK_AUTHORIZE_MISSING','SDK_EVENT_LISTENER_MISSING','SDK_UNAUTHORIZE_MISSING','SDK_EVENT_LISTENER_FAILED','SDK_INSTANCE_CHANGED','SDK_AUTHORIZE_REJECTED','AUTH_ATTEMPT_UNCONFIRMED'].includes(pair?.reason)){
                if(pair.reason!=='AUTH_ATTEMPT_UNCONFIRMED')diag(pair.reason);
                if(pair.reason==='SDK_AUTHORIZE_REJECTED'&&popupRequests===0&&popupCreations===0)diag('SDK_AUTHORIZE_REJECTED_BEFORE_POPUP');
                throw Error('SDK authorization unconfirmed');
            }return pair;
        },
        readSDK:()=>win.webContents.executeJavaScript(sdkRead),
        onInvalidate:reason=>{
            const code={'deadline':'AUTH_TIMEOUT','attempt-unconfirmed':'AUTH_ATTEMPT_UNCONFIRMED','web-session-changed':'WEB_SESSION_INVALIDATED','engine-restart':'AUTH_ATTEMPT_SUPERSEDED'}[reason];if(code)diag(code);
            if(['deadline','attempt-unconfirmed','web-session-changed','engine-restart'].includes(reason))void end();
        },
    });
    async function end(){
        if(ending)return ending;closing=true;context.invalidate('closing');for(const child of children)child.destroy();
        ending=(async()=>{await Promise.race([ses.clearStorageData().catch(()=>{}),new Promise(r=>setTimeout(r,1500))]);win?.destroy();app.exit(0);})();return ending;
    }
    ses.webRequest.onBeforeRequest((d,cb)=>{const allowed=webLoginRequestAllowed(d);if(!allowed){const classification=classifyWebLoginRequest(d,context.snapshot().state==='pending');diag(classification.requestClass==='AUTH_WINDOW_UNREVIEWED'?'AUTHORIZATION_REQUEST_BLOCKED':'REQUEST_BLOCKED',d.url,classification);}cb({cancel:!allowed});});
    ses.setPermissionRequestHandler((wc,p,cb)=>cb(['storage-access','top-level-storage-access'].includes(p)&&webLoginNavigation(wc.getURL())));
    ses.setPermissionCheckHandler((wc,p,origin)=>['storage-access','top-level-storage-access'].includes(p)&&webLoginNavigation(wc?.getURL())&&webLoginNavigation(origin));
    ses.on('will-download',(e)=>e.preventDefault());
    ses.cookies.on('changed',()=>context.sessionChanged()); // no cookie value is read/copied/logged
    win=new BrowserWindow({title:'Adam — Private Web Login',width:1100,height:780,show:true,
        webPreferences:{session:ses,preload,contextIsolation:true,nodeIntegration:false,sandbox:true,devTools:false,additionalArguments:['--adam-web-login=1']}});
    onWindow(win);win.webContents.setAudioMuted(true);
    function attach(wc,owner=null){
        wc.setWindowOpenHandler(({url})=>{
            if(context.snapshot().state==='pending')popupRequests++;
            if(probe||closing||context.snapshot().state!=='pending'||children.size>=2||!(url==='about:blank'||webLoginNavigation(url))){diag('POPUP_DENIED',url);return{action:'deny'};}
            return{action:'allow',overrideBrowserWindowOptions:{title:'Apple authorization — private session',autoHideMenuBar:true,
                webPreferences:{session:ses,preload:authPreload,contextIsolation:true,nodeIntegration:false,sandbox:true,devTools:false}}};
        });
        const gate=(kind,e,...args)=>{const {url}=webNavigationDetails(e,...args);
            const now=context.snapshot(),current=owner&&now.state==='pending'&&owner.attemptId===now.attemptId&&owner.epoch===now.epoch&&owner.document===now.document;
            if((!owner||current)&&(url==='about:blank'||webLoginNavigation(url)))return;
            if(owner){diag(kind==='redirect'?'POPUP_REDIRECT_FROM':'POPUP_NAVIGATION_FROM',wc.getURL());diag(current?'POPUP_ATTEMPT_CURRENT':'POPUP_ATTEMPT_STALE');}
            diag('NAVIGATION_BLOCKED',url);e.preventDefault();void end();};
        wc.on('will-navigate',(e,...args)=>gate('navigation',e,...args));wc.on('will-redirect',(e,...args)=>gate('redirect',e,...args));wc.on('will-attach-webview',e=>e.preventDefault());
        wc.on('did-create-window',child=>{popupCreations++;diag('POPUP_CREATED');children.add(child);child.webContents.setAudioMuted(true);attach(child.webContents,context.snapshot());child.once('closed',()=>children.delete(child));});
    }
    attach(win.webContents);
    win.webContents.on('did-start-navigation',(e,...args)=>{
        const navigation=webNavigationDetails(e,...args);
        if(!navigation.isMainFrame||navigation.isSameDocument)return;
        diag('MAIN_DOCUMENT_CHANGED');const active=context.snapshot().state!=='unverified';context.newDocument();if(active)void end();
    });
    win.webContents.on('render-process-gone',()=>{diag('RENDERER_EXITED');void end();});
    win.on('close',e=>{e.preventDefault();diag('PRIVATE_WINDOW_CLOSED');void end();});
    app.on('before-quit',e=>{e.preventDefault();void end();});
    async function authorize(){
        if(probe||closing||!win.webContents.mainFrame||new URL(win.webContents.getURL()).origin!=='https://music.apple.com')return;
        if(context.snapshot().state!=='unverified'||consentPending)return;
        consentPending=true;const doc=context.snapshot().document,frame=win.webContents.mainFrame;
        const confirmation=await dialog.showMessageBox(win,{type:'warning',buttons:['Cancel','Open Apple authorization'],defaultId:0,cancelId:0,
            message:'Web-only Apple authorization',detail:'Only continue if you choose to enter your Apple ID in Apple’s own interface. This temporary session cannot enable Native DRM, Lossless or Engine sources. Closing ends this private session. No passwords are saved by Adam.'});
        consentPending=false;
        if(confirmation.response!==1||closing)return;
        const stale=doc!==context.snapshot().document?'PRE_AUTH_DOCUMENT_STALE':frame!==win.webContents.mainFrame?'PRE_AUTH_FRAME_STALE':win.webContents.isLoadingMainFrame()?'PRE_AUTH_MAIN_LOADING':!webLoginNavigation(win.webContents.getURL())||new URL(win.webContents.getURL()).origin!=='https://music.apple.com'?'PRE_AUTH_ORIGIN_INVALID':null;
        if(stale){diag(stale);context.invalidate('pre-auth-stale');await dialog.showMessageBox(win,{type:'info',message:'Page changed before authorization',detail:'This authorization request was cancelled. Wait for the page to finish loading, then choose Authorize with Apple again.'});return;}
        try{popupRequests=0;popupCreations=0;diag('AUTH_ATTEMPT_STARTED');await context.begin({userGesture:true});if(!closing)await dialog.showMessageBox(win,{type:'info',message:'Current Web session verified by Apple API',detail:'This confirms only a usable MusicKit Web token for this temporary session. It does not identify a stable Apple ID or authorize Native DRM, Lossless or Engine playback.'});}catch{if(!closing){diag('AUTH_ATTEMPT_SUPERSEDED');void end();}}
    }
    async function signOut(){
        if(probe||closing)return;context.invalidate('manual-signout');
        // User explicitly requested SDK unauthorize, not device/DRM sign-out.
        await Promise.race([win.webContents.executeJavaScript(`(async()=>{try{await MusicKit.getInstance().unauthorize()}catch{}})()`).catch(()=>{}),new Promise(r=>setTimeout(r,3000))]);await end();
    }
    Menu.setApplicationMenu(Menu.buildFromTemplate([{label:'Private Web Account',submenu:[
        {label:'Authorize with Apple…',enabled:!probe,click:()=>void authorize()},
        {label:'Account status',click:()=>{const s=context.snapshot();void dialog.showMessageBox(win,{message:s.state==='authorized'?'Web session verified':'Web session not verified',detail:'Native DRM / Lossless / Engine Source Authority: BLOCKED'});}},
        {label:'Cancel and close private session',click:()=>void end()},
        {label:'Web sign out and close',enabled:!probe,click:()=>void signOut()},
        {type:'separator'},{label:'Close private instance',click:()=>void end()},
    ]}]));
    async function qualifySDK(){
        const doc=context.snapshot().document,frame=win.webContents.mainFrame;
        let reason='SDK_NOT_READY';
        for(let i=0;i<100&&!closing;i++){
            if(doc!==context.snapshot().document)return 'SDK_QUALIFICATION_DOCUMENT_REPLACED';
            if(frame!==win.webContents.mainFrame)return 'PRE_AUTH_FRAME_STALE';
            if(win.webContents.isLoadingMainFrame()){reason='SDK_MAIN_FRAME_LOADING';await new Promise(r=>setTimeout(r,100));continue;}
            let value;try{value=await win.webContents.executeJavaScript(sdkQualification)}catch{return 'SDK_INSTANCE_UNAVAILABLE'};
            if(closing||doc!==context.snapshot().document)return 'SDK_QUALIFICATION_DOCUMENT_REPLACED';
            if(frame!==win.webContents.mainFrame)return 'PRE_AUTH_FRAME_STALE';
            reason=value?.reason;
            if(reason==='SDK_INSTANCE_READY')return reason;
            await new Promise(r=>setTimeout(r,100));
        }
        return reason;
    }
    // This also checks the current manual-login document. A previous probe was
    // another process/document and cannot qualify this live SDK instance.
    if(!probe)win.webContents.on('did-finish-load',async()=>{if(context.snapshot().state!=='unverified')return;const reason=await qualifySDK();if(!closing)diag(reason)});
    if(probe){
        const watchdog=setTimeout(()=>{fs.writeFileSync('/evidence/probe.json',JSON.stringify({status:'BLOCKED',reason:'anonymous-page-timeout'}));void end();},40000);
        win.webContents.once('did-finish-load',async()=>{
            try{
                if(new URL(win.webContents.getURL()).origin!=='https://music.apple.com')throw Error();
                const sdkReason=await qualifySDK();diag(sdkReason);
                if(sdkReason!=='SDK_INSTANCE_READY'){
                    fs.writeFileSync('/evidence/probe.json',JSON.stringify({status:'BLOCKED',reason:sdkReason,authorizationEnabled:false}));
                    clearTimeout(watchdog);void end();return;
                }
                const sdkMethodsAvailable=true;
                const proof=await win.webContents.executeJavaScript(`({node:typeof require,bridge:typeof adamBridge,web:typeof adamWebLogin,isolation:window.adamWebLogin?.isolation,drmPatch:window.__adamDRMPatch===true})`);
                if(proof.node!=='undefined'||proof.bridge!=='undefined'||proof.web!=='object'||!proof.isolation?.sandboxed||!proof.isolation?.contextIsolated||proof.drmPatch)throw Error();
                fs.writeFileSync('/evidence/probe.json',JSON.stringify({status:'PASS',sdkMethodsAvailable,electron:process.versions.electron,memorySession:!ses.isPersistent(),sandboxed:win.webContents.getLastWebPreferences().sandbox,contextIsolated:true,engineStarted:false,authorizationEnabled:false,engineProcessCount:fs.readdirSync('/proc').filter(n=>/^[0-9]+$/.test(n)).filter(n=>{try{return fs.readFileSync('/proc/'+n+'/cmdline').includes(Buffer.from('/app/resources/engine'))}catch{return false}}).length,proof}));
            }catch{fs.writeFileSync('/evidence/probe.json',JSON.stringify({status:'BLOCKED',reason:'anonymous-boundary-unconfirmed'}));}
            clearTimeout(watchdog);void end();
        });
    }
    win.loadURL('https://music.apple.com/').catch(()=>{diag('MAIN_LOAD_FAILED');void end();});
    return {window:win,context,end};
}
