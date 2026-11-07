// Test-only assessment. Continued growth is a RED for investigation, not a
// diagnosis of a leak or a waiver based on caches/warmup. No forced GC.
export function assessResourceTrend(samples){
 if(samples.length<10)return {status:'NOT TESTED',reason:'insufficient real samples'};
 const end=samples.at(-1).wall;
 const checkpoints=[90,60,30,0].map(seconds=>samples.reduce((best,s)=>Math.abs(s.wall-(end-seconds*1000))<Math.abs(best.wall-(end-seconds*1000))?s:best,samples[0]));
 const reasons=[];
 for(const [group,key]of [['main','rssKiB'],['engine','rssKiB'],['main','heapUsed'],['engine','heapAlloc'],['engine','heapInuse'],['main','fd'],['engine','fd'],['engine','goroutines']]){
  const values=checkpoints.map(s=>s[group]?.[key]);
  if(values.every(Number.isFinite)&&values.slice(1).every((v,i)=>v>values[i]))reasons.push({group,key,values});
 }
 return {status:reasons.length?'RESOURCE TREND RED':'NO SUSTAINED GROWTH OBSERVED',windowMs:90000,reasons,checkpoints:checkpoints.map(s=>({wall:s.wall,main:s.main,engine:s.engine})),limitations:'finite run; not proof of leak freedom; any sustained positive trend kept RED'};
}
