import crypto from 'node:crypto';
import https from 'node:https';

// Main-only fixed Apple endpoint. No caller-controlled URL, redirects, cookies,
// error bodies or returned tokens. request injection is a trusted test seam.
export async function verifyAppleWebSession(pair,signal,request=https.request) {
    const token=v=>typeof v==='string'&&v.length>0&&v.length<=16384&&!/[\r\n\0]/.test(v);
    if(!pair||!token(pair.musicUserToken)||!token(pair.developerToken))throw Error('Web authorization unconfirmed');
    return new Promise((resolve,reject)=>{
        const fail=()=>reject(Error('Web authorization unconfirmed'));
        const req=request({hostname:'api.music.apple.com',port:443,path:'/v1/me/storefront',method:'GET',
            rejectUnauthorized:true,agent:false,signal:AbortSignal.any([signal,AbortSignal.timeout(5000)]),
            headers:{Authorization:'Bearer '+pair.developerToken,'Music-User-Token':pair.musicUserToken,Accept:'application/json'}},res=>{
            let size=0,body='';
            if(res.statusCode!==200||!/^application\/json(?:;|$)/i.test(res.headers['content-type']||'')){res.resume();fail();return;}
            res.setEncoding('utf8');res.on('data',chunk=>{size+=Buffer.byteLength(chunk);if(size>32768){req.destroy();fail();return;}body+=chunk;});
            res.on('error',fail);res.on('aborted',fail);res.on('end',()=>{
                try{if(!res.complete)throw Error();const value=JSON.parse(body);if(value.errors||!Array.isArray(value.data)||value.data.length!==1||value.data[0].type!=='storefronts'||!/^[a-z]{2}$/.test(value.data[0].id))throw Error();resolve({stage:'apple-user-endpoint-confirmed'});}catch{fail();}
            });
        });req.on('error',fail);req.end();
    });
}

// No public commit/result API. Only trusted Main binds SDK functions/verification.
// Context ID is an opaque web-session principal, NOT a stable Apple ID/device ID.
export function createWebAccountContext({authorizeSDK,readSDK,verify=verifyAppleWebSession,onInvalidate=()=>{},deadlineMs=300000}) {
    const instance=crypto.randomUUID();let epoch=0,document=0,sessionRevision=0,attempt=null,state='unverified',context=null;
    const snapshot=()=>({instance,epoch,document,sessionRevision,state,attemptId:attempt?.id??null,contextId:context,scope:'web-only',nativeDRM:false,sourceAuthority:false});
    function invalidate(reason='revoked') {epoch++;attempt?.abort.abort();clearTimeout(attempt?.timer);attempt=null;context=null;state='unverified';onInvalidate(reason);}
    function newDocument(){document++;invalidate('document-changed');}
    function check(a){if(attempt!==a||a.epoch!==epoch||a.document!==document||a.abort.signal.aborted||(a.sessionRevision!==undefined&&a.sessionRevision!==sessionRevision))throw Error('Web authorization superseded');}
    async function begin({userGesture=false}={}) {
        if(!userGesture||attempt||state==='authorized')throw Error('Web authorization unavailable');
        invalidate('new-intent');const a={id:crypto.randomUUID(),epoch,document,abort:new AbortController()};attempt=a;state='pending';
        a.timer=setTimeout(()=>invalidate('deadline'),deadlineMs);
        // Abort races resolve Main's wait; they do NOT roll back the SDK/Apple.
        const aborted=new Promise((_,reject)=>a.abort.signal.addEventListener('abort',()=>reject(Error('Web authorization superseded')),{once:true}));
        try {
            const pair=await Promise.race([Promise.resolve().then(()=>authorizeSDK()),aborted]);check(a);a.sessionRevision=sessionRevision;
            const confirmed=await Promise.race([verify(pair,a.abort.signal),aborted]);check(a);
            if(confirmed?.stage!=='apple-user-endpoint-confirmed')throw Error('Web authorization unconfirmed');
            const latest=await Promise.race([readSDK(),aborted]);check(a);
            if(latest?.musicUserToken!==pair.musicUserToken||latest?.developerToken!==pair.developerToken)throw Error('Web session changed');
            clearTimeout(a.timer);attempt=null;context=crypto.randomUUID();state='authorized';return snapshot();
        }catch{if(attempt===a)invalidate('attempt-unconfirmed');throw Error('Web authorization unconfirmed or superseded');}
    }
    return {begin,snapshot,invalidate,newDocument,
        sessionChanged(){sessionRevision++;if(state==='authorized'||(attempt&&attempt.sessionRevision!==undefined))invalidate('web-session-changed');},
        engineRestart(){invalidate('engine-restart');}};
}
