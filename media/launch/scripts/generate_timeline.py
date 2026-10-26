"""Generate explicit, individually editable scene/caption nodes from timing data."""
from pathlib import Path
import json
ROOT=Path(__file__).resolve().parents[1]
r=json.loads((ROOT/'src/timing.json').read_text())
lines=["import React from 'react';","import {Sequence,useVideoConfig} from 'remotion';","import {Audio} from '@remotion/media';","import {staticFile} from 'remotion';","import {Scene} from './Design';","import {Captions} from './Captions';","export const Launch=()=>{const {fps,width,height}=useVideoConfig();const portrait=height>width;return <>"]
for i,s in enumerate(r):
    lines.append(f'<Sequence name="{i+1:02} — {s["id"]}" from={{Math.round({s["startFrame"]}/30*fps)}} durationInFrames={{Math.round({s["frames"]}/30*fps)}}><Scene index={{{i}}}>')
    lines.append(f'<Audio src={{staticFile("{s["audio"]}")}} volume={{0.95}}/>')
    lines.append('<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={'+json.dumps(s['captions'],ensure_ascii=False)+'}/>')
    lines.append('</Scene></Sequence>')
lines.append('</>};')
(ROOT/'src/Launch.tsx').write_text('\n'.join(lines)+'\n')
frames=sum(s['frames'] for s in r)
(ROOT/'src/Root.tsx').write_text('''import React from 'react';
import {Composition,Still} from 'remotion';
import {Launch} from './Launch';
import {Poster} from './Design';
export const Root=()=> <>
<Composition id="Landscape" component={Launch} width={1920} height={1080} fps={30} durationInFrames={'''+str(frames)+'''}/>
<Composition id="Portrait" component={Launch} width={1080} height={1920} fps={30} durationInFrames={'''+str(frames)+'''}/>
<Still id="PosterLandscape" component={Poster} width={1920} height={1080}/>
<Still id="PosterPortrait" component={Poster} width={1080} height={1920}/>
</>;
''')
print(f'Generated {len(r)} scenes / {frames} frames.')
