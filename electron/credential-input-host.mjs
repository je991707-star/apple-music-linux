import path from 'node:path';
import {webNavigationDetails} from './private-web-login.mjs';
import crypto from 'node:crypto';
import {fileURLToPath,pathToFileURL} from 'node:url';

// Local app document only. Production installs enabled=false; no remote opener.
export function createCredentialInputHost({BrowserWindow,handle,accountHost,enabled=false}) {
    const dir=path.dirname(fileURLToPath(import.meta.url));
    const url=pathToFileURL(path.join(dir,'secure-credential.html')).href;
    let current=null;
    function retire(r) {
        if (!r || r.dead) return;
        r.dead=true;r.detachAbort?.();if(current===r)current=null;
        void accountHost.cancel(r.context).catch(()=>{});
        if(!r.win.isDestroyed())r.win.destroy();
    }
    function scope(event) {
        const r=current,wc=r?.win.webContents;
        if (!enabled || !r || r.dead || !r.ready || r.win.isDestroyed() || event.sender!==wc || event.senderFrame!==wc.mainFrame || wc.isDestroyed() || wc.getURL()!==url || event.senderFrame.url!==url) throw Error('Private input unavailable');
        accountHost.assertCurrent(r.context);
        const s=accountHost.authStatus(r.context);
        if(accountHost.current()!==r.context || s.binding!==r.attempt || !['accepted','running','challenge_required'].includes(s.phase)) throw Error('Private input unavailable');
        return r;
    }
    function register(name,fn) {
        handle('private-input:'+name,async(event,value)=>{
            try {const r=scope(event);return await fn(r,value);}
            catch {return {ok:false,error:'Private input unavailable'};}
            finally {if(value&&typeof value==='object')for(const k of ['email','password','reply'])if(Object.hasOwn(value,k))try{value[k]=''}catch{}}
        });
    }
    const exact=(v,keys)=>v&&Object.getPrototypeOf(v)===Object.prototype&&Object.keys(v).length===keys.length&&keys.every(k=>Object.hasOwn(v,k));
    register('credentials',(r,v)=>{if(!exact(v,['email','password']))throw Error();return accountHost.handoffCredential(r.context,r.attempt,v)});
    register('challenge',(r,v)=>{if(!exact(v,['challengeId','reply']))throw Error();return accountHost.handoffChallenge(r.context,r.attempt,v.challengeId,v.reply)});
    register('view',async r=>{const s=await accountHost.pollAuthentication(r.context,r.attempt);if(!['accepted','running','challenge_required'].includes(s.phase)||(s.phase==='challenge_required'&&!s.inputChallenge)){retire(r);return {ok:false,error:'Private input unavailable'}}return {ok:true,phase:s.phase,challenge:s.inputChallenge,nativeAuthenticated:false}});
    register('cancel',async r=>{retire(r);return {ok:true}});
    return Object.freeze({
        open(context,attempt) {
            if(!enabled||current||typeof accountHost.run!=='function')throw Error('Private input unavailable');
            accountHost.assertCurrent(context);
            const s=accountHost.authStatus(context);
            if(accountHost.current()!==context||s.binding!==attempt||!['accepted','running','challenge_required'].includes(s.phase))throw Error('Private input unavailable');
            const win=new BrowserWindow({width:440,height:480,show:false,title:'Offline Credential Handoff',webPreferences:{preload:path.join(dir,'secure-credential-preload.cjs'),partition:'u220-input-'+crypto.randomUUID(),sandbox:true,contextIsolation:true,nodeIntegration:false,webSecurity:true,devTools:false}});
            const r={win,context,attempt,ready:false,dead:false};current=r;
            const wc=win.webContents;
            // Main-owned context signal, not a Renderer-selected owner or epoch.
            void accountHost.run(context,({signal})=>{
                if(!signal||typeof signal.addEventListener!=='function')throw Error('Account cancellation unavailable');
                const abort=()=>retire(r);
                r.detachAbort=()=>signal.removeEventListener('abort',abort);
                signal.addEventListener('abort',abort,{once:true});
                r.bound=true;
                if(signal.aborted)retire(r);
            }).catch(()=>retire(r));
            const assets=new Set(['secure-credential.html','secure-credential.js','src/engine/account-sheet.js','src/engine/verification-code.js'].map(n=>pathToFileURL(path.join(dir,n)).href));
            wc.session.setPermissionRequestHandler((_w,_p,cb)=>cb(false));
            wc.session.webRequest.onBeforeRequest((details,cb)=>cb({cancel:!assets.has(details.url)}));
            wc.session.on('will-download',e=>e.preventDefault());
            wc.setWindowOpenHandler(()=>({action:'deny'}));
            wc.on('will-navigate',e=>{e.preventDefault();retire(r)});
            wc.on('did-start-navigation',(event,...args)=>{const n=webNavigationDetails(event,...args);if(n.isMainFrame&&(r.ready||n.url!==url))retire(r)});
            wc.on('did-finish-load',()=>{if(!r.dead&&r.bound&&wc.getURL()===url){r.ready=true;win.show()}else retire(r)});
            for(const name of ['render-process-gone','destroyed'])wc.on(name,()=>retire(r));
            // Only an explicit child-frame failure may leave the trusted Main alive.
            // Missing or malformed scope stays fail closed, as does a Main load failure.
            wc.on('did-fail-load',(_event,_errorCode,_errorDescription,_validatedURL,isMainFrame)=>{
                if(isMainFrame!==false)retire(r);
            });
            win.on('closed',()=>retire(r));
            if(!r.dead)win.loadFile(path.join(dir,'secure-credential.html')).catch(()=>retire(r));
            return Object.freeze({close:()=>retire(r)});
        },
        close(){retire(current)},
    });
}
