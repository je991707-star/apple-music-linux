"""Exercise the actual native token log statements in an in-memory stream.
No Vendor call, file-token access, or credential value appears in test output.
"""
from pathlib import Path
import argparse,json,subprocess
p=argparse.ArgumentParser();p.add_argument('--source-root',type=Path,required=True);p.add_argument('--output',type=Path,required=True);a=p.parse_args();a.output.mkdir(parents=True,exist_ok=False)
source=a.source_root/'drm/native/main.c';lines=source.read_text().splitlines()
markers={'debug':'DRM_DEBUG("[+] Music-Token:','cached':'fprintf(stderr, "[!] using cached MUSIC_TOKEN'}
selected={}
for case,marker in markers.items():
 matches=[(i+1,line.strip())for i,line in enumerate(lines)if marker in line]
 if len(matches)!=1:raise SystemExit('STOP: expected native log site changed: '+case)
 selected[case]=matches[0]
harness=a.output/'log-contract.c';harness.write_text('#define _GNU_SOURCE\n#include <stdio.h>\n#include <stdlib.h>\n#include <string.h>\nstatic FILE *capture;\n#define stderr capture\n#define DRM_DEBUG(...) fprintf(capture,__VA_ARGS__)\nint main(void){\n const char *g_music_token="u225-sensitive-native-token-synthetic";\n char prefix[15];memcpy(prefix,g_music_token,14);prefix[14]=0;\n int failed=0;\n'+''.join('{ char *data=NULL;size_t size=0;capture=open_memstream(&data,&size);if(!capture)return 2;\n'+stmt+'\nfclose(capture);if(strstr(data,prefix)||strstr(data,g_music_token)){puts("FAIL '+case+': token content reached native log formatter");failed=1;}else puts("PASS '+case+': native log omits token value");free(data);}\n'for case,(line,stmt)in selected.items())+'return failed;}\n')
cmd=['cc','-std=gnu11','-O1',str(harness),'-o',str(a.output/'log-contract')];r=subprocess.run(cmd,stdout=subprocess.PIPE,stderr=subprocess.STDOUT);(a.output/'build.log').write_bytes((repr(cmd)+'\n').encode()+r.stdout+f'\nEXIT={r.returncode}\n'.encode())
if r.returncode:raise SystemExit(r.returncode)
r=subprocess.run([str(a.output/'log-contract')],stdout=subprocess.PIPE,stderr=subprocess.STDOUT);(a.output/'run.log').write_bytes(r.stdout+f'\nEXIT={r.returncode}\n'.encode());(a.output/'SITES.json').write_text(json.dumps({case:{'line':line,'statement':stmt}for case,(line,stmt)in selected.items()},indent=2)+'\n');print(r.stdout.decode(),end='');raise SystemExit(r.returncode)
