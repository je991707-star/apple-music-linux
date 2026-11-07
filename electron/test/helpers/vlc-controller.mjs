// Execute production declarations selected by an AST; UI/provider boundaries only.
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import { createPlaybackCommands } from '../../src/engine/playback-commands.js';
import { createTransportController } from '../../src/engine/transport.js';
import{classifyVlcEnd}from'../../src/engine/vlc-end.js';
import{lengthAfterOverrun}from'../../src/engine/track-end.js';
const require=createRequire(process.env.ADAM_TEST_PARSER_BASE || import.meta.url);
const { parse } = require(process.env.ADAM_TEST_PARSER || 'espree');
const source = fs.readFileSync(process.env.ADAM_TEST_CONTROLLER_SOURCE || new URL("../../src/engine-playback.js",import.meta.url), 'utf8');
const ast = parse(source, { ecmaVersion: 'latest', sourceType: 'module' });
const names = new Set(['_resumeCurrentPositionSec','_touchSession','_reopenCurrentTrack','installMKSeekInterceptor', 'startVLCPoll', 'stopVLCPoll', '_requestVLCTransport', '_resetVLCSeekState', '_resetPlaybackState']);
const functions = ast.body.filter(n => n.type === 'FunctionDeclaration' && (names.has(n.id.name) || n.id.name.startsWith('_vlc')));
const globals = ast.body.filter(n => n.type === 'VariableDeclaration').flatMap(n => n.declarations).filter(n => n.id.type === 'Identifier').map(n => n.id.name);
const transport = ast.body.filter(n => n.type === 'VariableDeclaration').flatMap(n => n.declarations).find(n => n.id.name === '_vlcTransport');
export const extractedFunctions = functions.map(n => n.id.name);
export function controller(options = {}) {
    const timers = new Map(), commands = [], events = [], audio = new EventTarget();
    let id = 0, next = 0;
    const fetch = options.fetch || (async (url, opt) => {
        commands.push({ op: url.split('/').pop(), body: opt?.body && JSON.parse(opt.body) });
        return { ok: true, json: async () => url.endsWith('/time') ? { sessionId: 'a', controlRevision: 1 } : {} };
    });
    const sandbox = { URL, Event, AbortController, AbortSignal, performance, classifyVlcEnd,lengthAfterOverrun,_resumeSeekWhenReady:(_mk,v)=>events.push({reopen:v}),handleTrackChange:async()=>events.push('handle-track-change'), createTransportController, createPlaybackCommands, fetch, orderedEnabled:options.ordered===true, orderedFetch:options.orderedFetch,
        setTimeout: (fn, ms) => { const key = ++id; timers.set(key, { fn, ms }); return key; },
        clearTimeout: key => timers.delete(key), setInterval: () => ++id, clearInterval() {},
        _adamToast:()=>events.push('toast'), window: { adamBridge: { mprisUpdate() {} } }, console: { log() {}, warn() {} },
        ENGINE: options.origin || 'https://127.0.0.1:12345', getMKAudio: () => audio, T: () => ({ debounce: 150, poll: 250 }),
        _syncVLCControls() {}, _stopLyricsFreeze() {}, _startLyricsFreeze() {},
        _prewarmNextAlac: async () => events.push('prewarm-alac'), _prewarmNextMv: async () => events.push('prewarm-mv'),
        _adamNextRef: () => { next++; return Promise.resolve(); }, bridgeDuration() {}, unbridgeDuration() {},
        PLAY_STATE: { IDLE: 'idle' }, setPlayState() {}, deleteSession() {}, showQualityBadge() {},
    };
    const excluded = new Set(['_vlcCommands', '_vlcTransport', '_syncVLCControls', '_adamNextRef', 'ENGINE']);
    const src = `let ${globals.filter(n => !excluded.has(n)).map(n => n + '=null').join(',')};
        const ${source.slice(transport.start, transport.end)};
        const _vlcCommands=createPlaybackCommands({fetch,origin:ENGINE,scope:()=>({sessionId:_sessionId,loadGeneration:_vlcLoadGeneration,seekRevision:_vlcSeekRevision,transportRevision:_vlcTransport.revision}),onControl:_vlcObserveControl,enabled:orderedEnabled,orderedFetch});
        ${functions.map(n => source.slice(n.start, n.end)).join('\n')}
        _vlcSeekRevision=_vlcPollGeneration=_vlcRetryCount=_vlcPosMs=_vlcTickCount=_vlcErrCount=0;
        if (typeof _vlcLoadGeneration !== 'undefined') _vlcLoadGeneration=0;
        if (typeof _vlcRetryGeneration !== 'undefined') _vlcRetryGeneration=0;
        _vlcFacts={metadataDurationMs:null,sessionDurationMs:null,sampleDurationMs:null,sourceComplete:null,sourceFailed:null,readerEof:null,readerError:null,vlcLengthMs:null,terminalRawPosition:null};
        _vlcMode=true;_vlcSeekFrozen=false;_vlcPaused=false;_vlcFetching=false;
        const mk={seekToTime:async()=>{}};installMKSeekInterceptor(mk);
        ({
            session(id='a', ms=240000) {_sessionId=id;_currentAssetId='asset-'+id;_durationSec=ms/1000;_vlcMode=true;_mkInstance={nowPlayingItem:{id:_currentAssetId}};},
            update:(p,s='playing')=>_vlcUpdatePosition(p,s,getMKAudio()),
            stateChange:(s,p,old='playing')=>_vlcHandleStateChange(s,old,p,getMKAudio()),
            ended:(p,r='ended')=>_vlcHandleEnded(p,getMKAudio(),r),
            seek:p=>mk.seekToTime(p/1000),pause:()=>_requestVLCTransport(true),resume:()=>_requestVLCTransport(false),
            reset:_resetPlaybackState,start:()=>startVLCPoll(getMKAudio()),stopPoll:stopVLCPoll,
            poll:()=>_vlcPollTick(getMKAudio(),_sessionId),length:_vlcHandleLength,
            diagnostics:()=>_vlcDiagnosticSnapshot(),facts:v=>{_vlcFacts={..._vlcFacts,...v};if("readerEof"in v)_vlcSourceEof=v.readerEof;},touch:_touchSession,
            command:(type,body={})=>_vlcCommandFetch(ENGINE+"/api/v1/vlc/"+type,{method:"POST",body:JSON.stringify(body)}),
            beginLoad:()=>{if(typeof _vlcBeginLoad==='function')_vlcBeginLoad();},
            cdn:v=>{_allowCDNTransition=v;},
            snapshot:()=>({position:_vlcPosMs,retries:_vlcRetryCount,loading:_vlcLoading,duration:_durationSec,
                seek:_vlcSeekRevision,poll:_vlcPollGeneration,transport:_vlcTransport.revision})
        })`;
    const api = vm.runInNewContext(src, sandbox);
    api.session();
    return Object.assign(api, { timers, commands, events, audio, next: () => next,
        async run(ms) { const entry = [...timers].find(([,t]) => t.ms === ms); if (!entry) return false; timers.delete(entry[0]); await entry[1].fn(); return true; },
        async drain() { for (let i = 0; i < 8; i++) await new Promise(resolve => setImmediate(resolve)); },
    });
}

