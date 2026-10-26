import React from 'react';
import type {Caption} from '@remotion/captions';
import {useCurrentFrame, useVideoConfig} from 'remotion';

type Props = {
  captions: readonly Caption[];
  width: number;
  name?: string;
  style?: React.CSSProperties;
};

/** Original sentence-caption renderer. Timings come from measured PCM samples. */
export function Captions({captions, width, name, style}: Props) {
  const frame = useCurrentFrame();
  const {fps} = useVideoConfig();
  const milliseconds = Math.round(frame * 1000 / fps);
  const active = captions.find(c => c.startMs <= milliseconds && milliseconds < c.endMs);
  if (!active) return null;
  return <div data-caption-name={name} style={{width, display:'grid', placeItems:'center', ...style}}>
    <span style={{fontFamily:'"PingFang SC", "Noto Sans CJK SC", Arial, sans-serif',
      fontSize:64, lineHeight:1.3, color:'#f6faf5', textAlign:'center',
      background:'#14313bee', border:'1px solid #47626c', borderRadius:7,
      padding:'12px 23px', whiteSpace:'pre-wrap', textWrap:'balance'}}>{active.text}</span>
  </div>;
}
