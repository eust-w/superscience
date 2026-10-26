import React from 'react';
import {Sequence,useVideoConfig} from 'remotion';
import {Audio} from '@remotion/media';
import {staticFile} from 'remotion';
import {Scene} from './Design';
import {Captions} from './Captions';
export const Launch=()=>{const {fps,width,height}=useVideoConfig();const portrait=height>width;return <>
<Sequence name="01 — question" from={Math.round(0/30*fps)} durationInFrames={Math.round(209/30*fps)}><Scene index={0}>
<Audio src={staticFile("audio/scene-0.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "研究，不该只留下一个答案。", "startMs": 240, "endMs": 3237, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "还应该留下，答案从哪里来。", "startMs": 3397, "endMs": 6432, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
<Sequence name="02 — introduce" from={Math.round(209/30*fps)} durationInFrames={Math.round(327/30*fps)}><Scene index={1}>
<Audio src={staticFile("audio/scene-1.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "Superscience，一个本地运行的 AI 科研工作台。", "startMs": 240, "endMs": 4861, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "把文件、工具、Notebook 和证据，放进同一个项目。", "startMs": 5021, "endMs": 10358, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
<Sequence name="03 — onboarding" from={Math.round(536/30*fps)} durationInFrames={Math.round(264/30*fps)}><Scene index={2}>
<Audio src={staticFile("audio/scene-2.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "无需模型，也能打开本地工具。", "startMs": 240, "endMs": 3562, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "需要 AI 对话时，再验证你自己的模型连接。", "startMs": 3722, "endMs": 8265, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
<Sequence name="04 — search" from={Math.round(800/30*fps)} durationInFrames={Math.round(332/30*fps)}><Scene index={3}>
<Audio src={staticFile("audio/scene-3.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "检索、执行、分析。", "startMs": 240, "endMs": 2774, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "已有五十一种登记的科学数据连接器。", "startMs": 2934, "endMs": 6568, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "外部服务的可用性，仍需分别验证。", "startMs": 6728, "endMs": 10549, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
<Sequence name="05 — tools" from={Math.round(1132/30*fps)} durationInFrames={Math.round(325/30*fps)}><Scene index={4}>
<Audio src={staticFile("audio/scene-4.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "文件编辑、终端和 Notebook，围绕项目协作。", "startMs": 240, "endMs": 4909, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "工具操作经过权限审批，执行过程可以检查。", "startMs": 5069, "endMs": 10313, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
<Sequence name="06 — evidence" from={Math.round(1457/30*fps)} durationInFrames={Math.round(296/30*fps)}><Scene index={5}>
<Audio src={staticFile("audio/scene-5.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "把研究结果保存为产物。", "startMs": 240, "endMs": 2813, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "让来源、操作和结果相互连接，形成可以追溯的证据链。", "startMs": 2973, "endMs": 9338, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
<Sequence name="07 — architecture" from={Math.round(1753/30*fps)} durationInFrames={Math.round(263/30*fps)}><Scene index={6}>
<Audio src={staticFile("audio/scene-6.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "项目采用 Go、React 和 Wails。", "startMs": 240, "endMs": 3398, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "提供本地工作台，以及可选的自托管分享。", "startMs": 3558, "endMs": 8239, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
<Sequence name="08 — release" from={Math.round(2016/30*fps)} durationInFrames={Math.round(402/30*fps)}><Scene index={7}>
<Audio src={staticFile("audio/scene-7.wav")} volume={0.95}/>
<Captions name="中文字幕" width={portrait?1200:1800} style={{position:"absolute",bottom:portrait?245:105,left:portrait?90:285,scale:.75,transformOrigin:"left bottom"}} captions={[{"text": "源码现已公开，采用 Apache 二点零许可证。", "startMs": 240, "endMs": 4838, "timestampMs": null, "confidence": null, "pageBreakAfter": true}, {"text": "欢迎运行、检查代码，提交问题，和我们一起把科研流程做得更透明。", "startMs": 4998, "endMs": 12888, "timestampMs": null, "confidence": null, "pageBreakAfter": true}]}/>
</Scene></Sequence>
</>};
