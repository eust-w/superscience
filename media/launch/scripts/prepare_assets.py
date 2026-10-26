"""Rebuild synthetic Tingting narration and copy genuine product screenshots.
Requires macOS say, FFmpeg and FFprobe. No model credential is accessed.
"""
from pathlib import Path
import json, subprocess, wave, math, shutil, argparse
ROOT=Path(__file__).resolve().parents[1]
REPO=ROOT.parents[1]
AUDIO=ROOT/'public/audio'
AUDIO.mkdir(parents=True,exist_ok=True)
SCREEN=ROOT/'public/screenshots'
SCREEN.mkdir(parents=True,exist_ok=True)
for name in ['setup.jpg','workspace.jpg','notebook.jpg','search.jpg','artifacts.jpg']:
    source=REPO/'docs/readme'/('video-notebook.jpg' if name == 'notebook.jpg' else name)
    if source.exists(): shutil.copy2(source, SCREEN/name)
script=json.loads((ROOT/'narration.json').read_text())
parser=argparse.ArgumentParser();parser.add_argument('--scene',type=int);args=parser.parse_args()
cached=json.loads((ROOT/'src/timing.json').read_text()) if (ROOT/'src/timing.json').exists() else []
timing=[]; absolute=0; sr=48000; subtitles=[]
for n,scene in enumerate(script):
    if args.scene is not None and n != args.scene:
        prior=cached[n]
        for c in prior['captions']: subtitles.append((absolute+c['startMs']/1000,absolute+c['endMs']/1000,c['text']))
        timing.append({**prior,**scene,'startFrame':round(absolute*30)})
        absolute+=prior['frames']/30
        continue
    pcm=bytearray(b'\x00\x00'*round(sr*.24)); caps=[]
    for j,line in enumerate(scene['lines']):
        aiff=AUDIO/f'{n:02}-{j:02}.aiff'; wav=AUDIO/f'{n:02}-{j:02}.wav'
        subprocess.run(['say','-v','Tingting','-r','145','-o',str(aiff),line],check=True)
        subprocess.run(['ffmpeg','-v','error','-y','-i',str(aiff),'-ar',str(sr),'-ac','1','-c:a','pcm_s16le',str(wav)],check=True)
        with wave.open(str(wav),'rb') as inp: data=inp.readframes(inp.getnframes())
        start=len(pcm)/2/sr; pcm.extend(data); end=len(pcm)/2/sr
        caps.append({'text':line,'startMs':round(start*1000),'endMs':round(end*1000),'timestampMs':None,'confidence':None,'pageBreakAfter':True})
        subtitles.append((absolute+start,absolute+end,line))
        pcm.extend(b'\x00\x00'*round(sr*.16))
    pcm.extend(b'\x00\x00'*round(sr*.35))
    durationFrames=math.ceil(len(pcm)/2/sr*30)
    targetSamples=round(durationFrames/30*sr)
    pcm.extend(b'\x00\x00'*max(0,targetSamples-len(pcm)//2))
    with wave.open(str(AUDIO/f'scene-{n}.wav'),'wb') as out:
        out.setnchannels(1);out.setsampwidth(2);out.setframerate(sr);out.writeframes(pcm)
    timing.append({**scene,'startFrame':round(absolute*30),'frames':durationFrames,'audio':f'audio/scene-{n}.wav','captions':caps})
    absolute+=durationFrames/30
    for p in AUDIO.glob(f'{n:02}-*'): p.unlink()
(ROOT/'src/timing.json').write_text(json.dumps(timing,ensure_ascii=False,indent=2)+'\n')
def ts(s):
    ms=round(s*1000); h,ms=divmod(ms,3600000);m,ms=divmod(ms,60000);s,ms=divmod(ms,1000)
    return f'{h:02}:{m:02}:{s:02},{ms:03}'
srt='\n'.join(f'{i+1}\n{ts(start)} --> {ts(end)}\n{text}\n' for i,(start,end,text) in enumerate(subtitles))
(ROOT/'subtitles.zh-CN.srt').write_text(srt)
(REPO/'.qa/video').mkdir(parents=True,exist_ok=True)
(REPO/'.qa/video/subtitles.zh-CN.srt').write_text(srt)
print(json.dumps({'seconds':absolute,'frames':round(absolute*30),'scenes':len(timing),'syntheticVoice':'macOS Tingting'},ensure_ascii=False))

subprocess.run(["python3", str(ROOT/"scripts/generate_timeline.py")],check=True)
