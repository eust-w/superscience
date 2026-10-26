"""Validate finished delivery encodes and write a machine-readable manifest."""
from pathlib import Path
import hashlib,json,subprocess
ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT.parents[1]/'.qa/video'
expected={'superscience-launch-1080p.mp4':(1920,1080),'superscience-launch-vertical.mp4':(1080,1920)}
expected_duration=sum(s['frames'] for s in json.loads((ROOT/'src/timing.json').read_text()))/30
result=[]
for name,(w,h) in expected.items():
    path=OUT/name
    info=json.loads(subprocess.check_output(['ffprobe','-v','error','-show_format','-show_streams','-of','json',str(path)]))
    v=next(s for s in info['streams'] if s['codec_type']=='video')
    a=next(s for s in info['streams'] if s['codec_type']=='audio')
    duration=float(info['format']['duration'])
    assert (v['width'],v['height'])==(w,h)
    assert v['codec_name']=='h264' and v['pix_fmt']=='yuv420p'
    assert v['avg_frame_rate']=='30/1'
    assert a['codec_name']=='aac'
    assert abs(duration-expected_duration)<.1
    result.append({'file':name,'bytes':path.stat().st_size,'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'duration_seconds':duration,'width':w,'height':h,'fps':30,'video_codec':v['codec_name'],'pixel_format':v['pix_fmt'],'audio_codec':a['codec_name'],'audio_sample_rate':a['sample_rate'],'audio_channels':a['channels']})
for name in ['poster-landscape.png','poster-portrait.png','subtitles.zh-CN.srt']:
    p=OUT/name
    result.append({'file':name,'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()})
manifest={'synthetic_narration':True,'synthetic_demo_measurements':True,'external_model_answer_verified':False,'music':None,'files':result}
(OUT/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(manifest,ensure_ascii=False,indent=2))
