// Strict evidence oracle. It does not issue playback commands or model EOF policy.
const id=v=>typeof v==='string'&&v.length>0;
const demand=(v,message)=>{if(!v)throw Error(message)};
export function verifyFormalNext({a,b,aEOF,next,cleanup,expectedBHash}) {
 demand(a&&b&&a.controller&&b.controller,'Missing source observations');
 const as=a.controller.sessionId,bs=b.controller.sessionId;
 demand(id(as)&&id(bs)&&as!==bs,'B Session was not acquired');
 demand(a.controller.assetId==='alac-a'&&b.controller.assetId==='alac-b','Wrong assets');
 const ae=a.engine,be=b.engine,ap=ae?.pcm,bp=be?.pcm,n=bp?.native,l=n?.lease,binding=bp?.binding;
 demand(ae?.sessionId===as&&be?.sessionId===bs&&be.control?.sessionId===bs,'Engine is not B');
 demand(ap?.native?.lease?.sessionId===as&&ap.native.lease.sourceGeneration===ae.sourceGeneration&&aEOF?.binding?.leaseId===ap.native.lease.leaseId,'A Native identity missing');
 demand(bp?.coherent===true&&n?.callbackAttached===true&&id(n?.physicalMedia),'No coherent attached Native B');
 demand(l?.sessionId===bs&&l.leaseId===binding?.leaseId&&binding?.sessionId===bs&&binding.assetId==='alac-b','Native lease is not B');
 demand(Number.isSafeInteger(be.sourceGeneration)&&be.sourceGeneration>ae.sourceGeneration&&l.sourceGeneration===be.sourceGeneration&&binding.sourceGeneration===be.sourceGeneration,'Wrong source generation');
 demand(l.boot===be.control.boot,'Wrong Native Boot');
 demand(id(l.leaseId)&&l.leaseId!==ap.native.lease.leaseId,'Reused A lease');
 demand(n.physicalLoad>ap.native.physicalLoad&&n.callbackHandle!==ap.native.callbackHandle,'No new physical callback media');
 demand(be.control.loadGeneration===b.controller.loadGeneration&&be.control.sequence===binding.commandSequence,'Wrong command identity');
 demand(bp.resetSerial>ap.resetSerial&&bp.resetSerial!==aEOF?.resetSerial,'A sink not reset');
 demand(aEOF?.sessionId===as&&aEOF.full_pcm_exact===true&&aEOF.frames===192000&&aEOF.sha256===ap.referenceSHA256&&aEOF.events?.some(e=>e.event==='MediaPlayerEndReached'&&e.resetSerial===aEOF.resetSerial),'No A natural Native EOF');
 demand(next?.count===1&&next.sessionId===as,'Duplicate or missing formal Next');
 demand(bp.events?.some(e=>e.event==='MediaPlayerPlaying'&&e.resetSerial===bp.resetSerial)&&bp.events.some(e=>e.event==='MediaPlayerEndReached'&&e.resetSerial===bp.resetSerial),'No B Native events');
 demand(id(expectedBHash)&&expectedBHash!==ap.referenceSHA256&&bp.referenceSHA256===expectedBHash&&bp.sha256===expectedBHash&&bp.frames===192000&&bp.full_pcm_exact===true,'PCM does not identify B');
 demand(be.state==='ended','B Native has not reached EOF');
 demand(be.sourceComplete===true&&be.sourceFailed===false&&be.eof===true,'B data/EOF not confirmed');
 demand(cleanup?.bStillCurrent===true&&cleanup.oldReleaseRejected===true&&cleanup.old404Rejected===true,'Old A work affected B');
 return true;
}
