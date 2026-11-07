// Private opt-in diagnostics: fixed codes only; never serialize event/request,
// exception, SDK result, cookie, header or full URL.
const codes=new Set(['POPUP_REDIRECT_FROM','POPUP_NAVIGATION_FROM','POPUP_ATTEMPT_CURRENT','POPUP_ATTEMPT_STALE','PRE_AUTH_DOCUMENT_STALE','PRE_AUTH_FRAME_STALE','PRE_AUTH_MAIN_LOADING','PRE_AUTH_ORIGIN_INVALID','SDK_QUALIFICATION_DOCUMENT_REPLACED','PRIVATE_WINDOW_CLOSED','MAIN_LOAD_FAILED','SDK_MAIN_FRAME_LOADING','SDK_GET_INSTANCE_THREW','SDK_INSTANCE_NULL','SDK_AUTHORIZE_MISSING','SDK_EVENT_LISTENER_MISSING','SDK_UNAUTHORIZE_MISSING','SDK_EVENT_LISTENER_FAILED','SDK_INSTANCE_CHANGED','SDK_INSTANCE_READY','SDK_DOCUMENT_CHANGED','SDK_NOT_READY','SDK_INSTANCE_UNAVAILABLE','SDK_AUTHORIZE_REJECTED_BEFORE_POPUP','AUTHORIZATION_REQUEST_BLOCKED','AUTH_ATTEMPT_STARTED','POPUP_CREATED','POPUP_DENIED','NAVIGATION_BLOCKED','REQUEST_BLOCKED','MAIN_DOCUMENT_CHANGED','WEB_SESSION_INVALIDATED','SDK_AUTHORIZE_REJECTED','AUTH_ATTEMPT_SUPERSEDED','AUTH_ATTEMPT_UNCONFIRMED','AUTH_TIMEOUT','RENDERER_EXITED']);
export function safeWebDiagnosticHost(value){
    try{const h=new URL(value).hostname.toLowerCase();
        if(h.length<=100&&/^[a-z0-9.-]+$/.test(h)&&(h==='apple.com'||h.endsWith('.apple.com')||h==='appleid.cdn-apple.com'||h.endsWith('.mzstatic.com')))return h;
        return 'OTHER_HOST';
    }catch{return undefined;}
}
const methods=new Set(['GET','POST','OPTIONS','HEAD','PUT','DELETE','PATCH']);
const types=new Set(['mainFrame','subFrame','stylesheet','script','image','font','object','xhr','ping','cspReport','media','webSocket','other']);
const classes=new Set(['FONT_ASSET','CAMPAIGN_REGISTRATION','MARKETING_UPSELL','AUTH_WINDOW_UNREVIEWED','PAGE_UNREVIEWED']);
export function createWebLoginDiagnostics({enabled=false,write=()=>{}}={}){
    let count=0;const requests=new Set();
    return(code,value,metadata)=>{
        if(!enabled||!codes.has(code)||count>=128)return;
        const hostname=value===undefined?undefined:safeWebDiagnosticHost(value);
        const classification=metadata?{method:methods.has(metadata.method)?metadata.method:'OTHER',resourceType:types.has(metadata.resourceType)?metadata.resourceType:'OTHER',requestClass:classes.has(metadata.requestClass)?metadata.requestClass:'PAGE_UNREVIEWED'}:{};
        if(code==='REQUEST_BLOCKED'||code==='AUTHORIZATION_REQUEST_BLOCKED'){
            const key=JSON.stringify([code,hostname,classification]);if(requests.has(key)||requests.size>=32)return;requests.add(key);
        }
        const row={reasonCode:code,...(hostname?{hostname}:{}),...classification};count++;
        try{write(row);}catch{} // diagnostics failure must not disclose errors or grant auth
    };
}
