import{test}from'node:test';import assert from'node:assert/strict';import{assessResourceTrend}from'../../verification/u2-source/resource-trend.mjs';
const samples=fn=>Array.from({length:25},(_,i)=>({wall:i*10000,main:{rssKiB:fn(i),fd:20},engine:{rssKiB:100,fd:10,goroutines:5}}));
test('continued RSS growth produces an investigation RED without a cache waiver',()=>assert.equal(assessResourceTrend(samples(i=>100+i)).status,'RESOURCE TREND RED'));
test('finite plateau is recorded without claiming universal leak freedom',()=>{const r=assessResourceTrend(samples(i=>i<10?100+i:110));assert.equal(r.status,'NO SUSTAINED GROWTH OBSERVED');assert.match(r.limitations,/not proof/)});

test('continuous Main heap growth remains RED even when RSS plateaus',()=>{const rows=Array.from({length:10},(_,i)=>({wall:i*10000,main:{rssKiB:100,heapUsed:1000+i*100,fd:10},engine:{rssKiB:100,heapAlloc:1000,heapInuse:2000,fd:10,goroutines:5}}));const v=assessResourceTrend(rows);assert.equal(v.status,'RESOURCE TREND RED');assert(v.reasons.some(r=>r.group==='main'&&r.key==='heapUsed'))});
