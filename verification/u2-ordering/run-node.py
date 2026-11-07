from pathlib import Path
import os,subprocess,shutil,json
R=Path(__file__).resolve().parents[2];L=R/'.u2-lab/u2-1-20261102-run1';H=L/'controller-u1';H.mkdir(exist_ok=True);OLD=Path('/var/home/h97m/Projects/adam-upstream-review-output/phase-u1/tests')
for name in ['upstream-controller.mjs','controller-contracts.test.mjs','controller-probes.test.mjs']:
 s=(OLD/name).read_text().replace('/var/home/h97m/Projects/adam-upstream-150-review',str(R));(H/name).write_text(s)
env={**os.environ,'ELECTRON_RUN_AS_NODE':'1','HOME':str(L/'home'),'TMPDIR':str(L/'tmp'),'NODE_PATH':'/var/home/h97m/Projects/adam/electron/node_modules'}
node='/var/home/h97m/Projects/adam/electron/dist/linux-unpacked/adam'
results={}
for name,args in [('node-full-retry',['--test','--test-isolation=none']+[str(p)for p in sorted((R/'electron/test').rglob('*.test.mjs'))]),('u1-observations-retry',['--test','--test-isolation=none',str(H/'controller-probes.test.mjs')]),('u1-contracts-red-retry',['--test','--test-isolation=none',str(H/'controller-contracts.test.mjs')])]:
 with(L/(name+'.log')).open('w')as f:
  p=subprocess.run([node,*args],env=env,stdout=f,stderr=subprocess.STDOUT,timeout=90,cwd=R/'electron')
 results[name]=p.returncode
(L/'node-exits-retry.json').write_text(json.dumps(results,indent=2));print(results)
