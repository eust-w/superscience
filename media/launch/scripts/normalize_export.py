"""Normalize H.264 range metadata and MP4 layout for creator-platform uploads."""
from pathlib import Path
import subprocess,json,os,sys
p=Path(sys.argv[1]).resolve()
info=json.loads(subprocess.check_output(['ffprobe','-v','error','-select_streams','v:0','-show_streams','-of','json',str(p)]))['streams'][0]
if info['pix_fmt']=='yuv420p' and info.get('color_range')=='tv':
    print('Already normalized:',p.name);raise SystemExit(0)
out=p.with_name(p.stem+'.normalized.mp4')
in_range='pc' if info.get('color_range')=='pc' or info['pix_fmt']=='yuvj420p' else 'tv'
subprocess.run(['ffmpeg','-v','error','-y','-threads','2','-i',str(p),'-vf',f'scale=in_range={in_range}:out_range=tv,format=yuv420p','-c:v','libx264','-threads','2','-preset','fast','-crf','17','-pix_fmt','yuv420p','-color_range','tv','-colorspace','bt709','-color_primaries','bt709','-color_trc','bt709','-c:a','copy','-movflags','+faststart',str(out)],check=True)
check=json.loads(subprocess.check_output(['ffprobe','-v','error','-select_streams','v:0','-show_streams','-of','json',str(out)]))['streams'][0]
assert check['pix_fmt']=='yuv420p'
os.replace(out,p)
print('Normalized:',p.name)
