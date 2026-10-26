import React from 'react';
import {AbsoluteFill, CanvasImage, Easing, interpolate, staticFile, useCurrentFrame, useVideoConfig, getStaticFiles} from 'remotion';
import timeline from './timing.json';

const C={bg:'#071c24',ink:'#f2f5ed',muted:'#a2b9ba',teal:'#6cdbca',line:'#28464d',gold:'#edb976'};
const font='"PingFang SC", "Noto Sans CJK SC", "Microsoft YaHei", Arial, sans-serif';
const ease=Easing.bezier(.16,1,.3,1);
const clamp={extrapolateLeft:'clamp' as const,extrapolateRight:'clamp' as const,easing:ease};

export const Mark=({size=54}:{size?:number})=><svg width={size} height={size} viewBox="0 0 100 100"><circle cx="50" cy="50" r="40" fill="none" stroke={C.teal} strokeWidth="3"/><path d="M25 65 L50 20 L75 65 Z M24 37 H76 M35 80 L50 51 L65 80" stroke={C.teal} strokeWidth="3" fill="none"/><circle cx="50" cy="51" r="7" fill={C.gold}/></svg>;

const Grid=()=>{const f=useCurrentFrame();return <AbsoluteFill style={{backgroundImage:'linear-gradient(#4c887a15 1px, transparent 1px),linear-gradient(90deg,#4c887a15 1px, transparent 1px)',backgroundSize:'80px 80px',backgroundPosition:`${f*.1}px ${f*.05}px`,maskImage:'linear-gradient(90deg,transparent,#000)'}}/>};
const Reveal=({children,delay=0,style={}}:{children:React.ReactNode;delay?:number;style?:React.CSSProperties})=>{const f=useCurrentFrame();return <div style={{...style,opacity:interpolate(f,[delay,delay+22],[0,1],clamp),translate:interpolate(f,[delay,delay+30],['0px 30px','0px 0px'],clamp)}}>{children}</div>};
const Mono=({children,style={}}:{children:React.ReactNode;style?:React.CSSProperties})=><div style={{fontFamily:'"SF Mono", Menlo, monospace',letterSpacing:2,fontSize:24,color:C.teal,...style}}>{children}</div>;
const Chip=({children,color=C.teal}:{children:React.ReactNode;color?:string})=><div style={{border:`1px solid ${color}55`,background:`${color}12`,color,padding:'12px 22px',borderRadius:99,fontSize:28,fontWeight:600}}>{children}</div>;
const Card=({title,subtitle,number,delay=0}:{title:string;subtitle:string;number:string;delay?:number})=><Reveal delay={delay} style={{padding:'30px 34px',border:`1px solid ${C.line}`,background:'#0e2b34',borderRadius:16,display:'flex',alignItems:'center',gap:28}}><Mono style={{fontSize:24,color:C.gold}}>{number}</Mono><div><div style={{fontSize:40,fontWeight:650,color:C.ink}}>{title}</div><div style={{fontSize:25,color:C.muted,marginTop:9}}>{subtitle}</div></div></Reveal>;

function EvidenceOrbit({portrait,settled=false}:{portrait:boolean;settled?:boolean}){
 const rawFrame=useCurrentFrame(); const f=settled?100:rawFrame; const size=portrait?720:740;
 const nodes=[{x:370,y:105,label:'问题',en:'QUESTION'},{x:615,y:440,label:'来源',en:'SOURCES'},{x:125,y:440,label:'证据',en:'EVIDENCE'}];
 return <div style={{width:size,height:600,position:'relative'}}><svg width={size} height="600" viewBox="0 0 740 600"><circle cx="370" cy="310" r="206" stroke={C.line} strokeWidth="2" fill="none"/><circle cx="370" cy="310" r="250" stroke={C.line} strokeWidth="1" strokeDasharray="4 20" fill="none"/><path d="M370 105L615 440H125Z" fill="none" stroke={C.teal} strokeWidth="3" strokeDasharray="1400" strokeDashoffset={interpolate(f,[10,70],[1400,0],clamp)}/><circle cx="370" cy="310" r="70" fill="#173d45" stroke={C.teal} strokeWidth="2"/><text x="370" y="323" textAnchor="middle" fontSize="40" fill={C.ink}>TRACE</text>{nodes.map((n,i)=><g key={n.label} opacity={interpolate(f,[i*8,i*8+20],[0,1],clamp)}><circle cx={n.x} cy={n.y} r="62" fill={C.bg} stroke={i===0?C.gold:C.teal} strokeWidth="2"/><text x={n.x} y={n.y+8} textAnchor="middle" fontSize="36" fill={C.ink}>{n.label}</text><text x={n.x} y={n.y+88} textAnchor="middle" fontSize="21" fill={C.muted} letterSpacing="2">{n.en}</text></g>)}</svg></div>
}

function Shot({file,label,note,portrait,motionStart=0}:{file:string;label:string;note:string;portrait:boolean;motionStart?:number}){
 const f=Math.max(0,useCurrentFrame()-motionStart);
 return <div style={{width:'100%'}}><div style={{padding:12,border:`1px solid ${C.teal}66`,borderRadius:18,background:'#17343b',boxShadow:'0 30px 70px #0005',scale:interpolate(f,[0,160],[.97,1],clamp)}}><div style={{aspectRatio:'16 / 9',overflow:'hidden',borderRadius:10}}><CanvasImage src={staticFile('screenshots/'+file)} style={{display:'block',width:'100%',scale:file==='setup.jpg'?1:interpolate(f,[35,115],[1,2],clamp),transformOrigin:file==='notebook.jpg'?'right 82%':'right 50%'}}/></div></div><div style={{marginTop:26,fontSize:portrait?32:27,lineHeight:1.7,color:C.teal}}>{label}</div><div style={{marginTop:12,fontSize:portrait?27:24,lineHeight:1.6,color:C.gold}}>{note}</div></div>
}
const hasShot=(name:string)=>getStaticFiles().some(x=>x.name==='screenshots/'+name);
function Visual({index,portrait}:{index:number;portrait:boolean}){
 const f=useCurrentFrame();
 if(index===0)return <EvidenceOrbit portrait={portrait}/>;
 if(index===1)return <div style={{width:'100%'}}><Mono style={{marginBottom:22}}>ONE PROJECT / 一处工作空间</Mono><div style={{display:'grid',gridTemplateColumns:portrait?'1fr':'1fr 1fr',gap:18}}><Card number="01" title="文件" subtitle="项目上下文" delay={4}/><Card number="02" title="工具" subtitle="真实终端与执行" delay={12}/><Card number="03" title="Notebook" subtitle="逐步分析与输出" delay={20}/><Card number="04" title="证据" subtitle="产物与来源" delay={28}/></div><Mono style={{marginTop:24,fontSize:22,color:C.muted}}>工作流示意 · 非运行结果</Mono></div>;
 if(index===2)return <Shot file={f<120?'setup.jpg':'notebook.jpg'} motionStart={f<120?0:120} portrait={portrait} label={f<120?'真实界面 · 模型配置':'真实界面 · 本地 Notebook'} note={f<120?'AI 功能需要有效模型配置与验证':'无需模型可运行本地工具 · 合成演示数据'}/>;
 if(index===3&&f>145&&hasShot('search.jpg'))return <Shot file="search.jpg" motionStart={145} portrait={portrait} label="真实检索 · Crossref" note="机器人学习文献 · 无模型生成"/>;
 if(index===3)return <div style={{width:'100%'}}><Reveal><div style={{display:'flex',alignItems:'baseline',gap:25}}><div style={{fontSize:portrait?230:240,fontWeight:750,letterSpacing:-16,lineHeight:.9,color:C.teal}}>51</div><div style={{fontSize:40,lineHeight:1.35}}>登记的<br/>科学连接器</div></div></Reveal><div style={{display:'flex',flexWrap:'wrap',gap:14,marginTop:50}}>{['文献','数据集','物理','材料','机器人','生命科学'].map((x,i)=><Reveal key={x} delay={i*5+10}><Chip>{x}</Chip></Reveal>)}</div><div style={{borderLeft:`4px solid ${C.gold}`,paddingLeft:22,fontSize:27,color:C.gold,marginTop:35,lineHeight:1.6}}>登记数量不等于全部在线验证<br/>数据来源与失败状态保持可见</div></div>;
 if(index===4)return <div style={{width:'100%'}}><Shot file="notebook.jpg" portrait={portrait} label="真实 Notebook 执行" note="无真实模型回答 · 合成数据，非性能基准"/><div style={{display:'flex',gap:14,marginTop:24,flexWrap:'wrap'}}><Chip>文件</Chip><Chip>终端</Chip><Chip>审批 ≠ 沙箱</Chip></div></div>;
 if(index===5&&f>135&&hasShot('artifacts.jpg'))return <Shot file="artifacts.jpg" motionStart={135} portrait={portrait} label="真实产物 · 本地分享回放" note="本地分享 · 无模型生成 · 非公开 Cloud 部署"/>;
 if(index===5)return <div style={{width:'100%'}}>{[['来源','文献 / 数据 / 文件'],['操作','检索 / 分析 / 执行'],['产物','结果 / 导出 / 分享']].map(([title,sub],i)=><React.Fragment key={title}><Card number={`0${i+1}`} title={title} subtitle={sub} delay={i*15}/>{i<2?<div style={{fontSize:34,color:C.teal,margin:'4px 0 4px 54px',opacity:interpolate(f,[15+i*15,35+i*15],[0,1],clamp)}}>↓</div>:null}</React.Fragment>)}<Mono style={{marginTop:22,fontSize:22,color:C.muted}}>证据链示意 · 可检查的关联</Mono></div>;
 if(index===6)return <div style={{width:'100%'}}><Card number="UI" title="React + Wails" subtitle="浏览器与桌面工作台"/><div style={{height:26,borderLeft:`2px solid ${C.teal}`,marginLeft:65}}/><Card number="API" title="Go runtime" subtitle="工具 · 权限 · 连接器" delay={12}/><div style={{height:26,borderLeft:`2px solid ${C.teal}`,marginLeft:65}}/><div style={{display:'grid',gridTemplateColumns:'1fr 1fr',gap:16}}><Reveal delay={24} style={{border:`1px solid ${C.line}`,borderRadius:14,padding:'25px 20px',background:'#0e2b34'}}><div style={{fontSize:38,fontWeight:650}}>SQLite</div><div style={{fontSize:24,color:C.muted,marginTop:10}}>本地持久化</div></Reveal><Reveal delay={32} style={{border:`1px dashed ${C.line}`,borderRadius:14,padding:'25px 20px'}}><div style={{fontSize:38,fontWeight:650}}>Cloud</div><div style={{fontSize:24,color:C.muted,marginTop:10}}>可选 · 自托管</div></Reveal></div><Mono style={{marginTop:25,fontSize:22,color:C.muted}}>架构示意</Mono></div>;
 return <div style={{width:'100%'}}><Mark size={140}/><div style={{fontSize:portrait?74:80,fontWeight:700,letterSpacing:-3,marginTop:30}}>Superscience</div><div style={{fontSize:portrait?34:32,color:C.teal,marginTop:25,fontFamily:'"SF Mono", Menlo, monospace',lineHeight:1.6}}>github.com/eust-w/<br/>superscience</div><div style={{display:'flex',gap:12,flexWrap:'wrap',marginTop:30}}><Chip>Apache 2.0</Chip><Chip color={C.gold}>运行 · 阅读 · 共建</Chip></div></div>
}

export const Scene=({index,children}:{index:number;children?:React.ReactNode})=>{
 const {width,height}=useVideoConfig();const portrait=height>width;const data=timeline[index];const f=useCurrentFrame();
 return <AbsoluteFill style={{background:C.bg,color:C.ink,fontFamily:font,overflow:'hidden'}}>
  <Grid/>
  <div style={{position:'absolute',right:-200,top:portrait?650:120,width:1000,height:1000,borderRadius:'50%',background:'radial-gradient(closest-side,#1a615d22,transparent)'}}/>
  <div style={{position:'absolute',left:portrait?84:100,top:portrait?95:60,display:'flex',gap:17,alignItems:'center'}}><Mark size={portrait?54:46}/><span style={{fontSize:portrait?34:30,fontWeight:650,letterSpacing:-.6}}>Superscience</span></div>
  <Mono style={{position:'absolute',right:portrait?84:100,top:portrait?111:74,fontSize:22,color:C.muted}}>{String(index+1).padStart(2,'0')} / 08</Mono>
  <div style={{position:'absolute',left:portrait?84:100,top:portrait?245:235,width:portrait?912:860}}>
   <Reveal><Mono style={{fontSize:portrait?22:24,color:C.gold,letterSpacing:portrait?1:2}}>{data.eyebrow}</Mono></Reveal>
   <Reveal delay={5}><h1 style={{fontSize:portrait?90:(index===0?120:96),fontWeight:700,letterSpacing:-4,lineHeight:1.2,whiteSpace:'pre-line',margin:'28px 0 30px'}}>{data.title}</h1></Reveal>
   {!portrait&&<Reveal delay={17}><div style={{width:110,height:4,background:C.teal,marginTop:44}}/></Reveal>}
   {index===0&&<Reveal delay={22}><div style={{fontSize:portrait?32:31,color:C.muted,lineHeight:1.6,marginTop:40}}>A LOCAL AI WORKBENCH<br/>FOR TRACEABLE RESEARCH</div></Reveal>}
  </div>
  <div style={{position:'absolute',left:portrait?100:1020,top:portrait?(index===0?790:710):220,width:portrait?880:790,height:portrait?800:650,display:'flex',alignItems:portrait?'flex-start':'center',justifyContent:'center'}}><Visual index={index} portrait={portrait}/></div>
  {children}
  <div style={{position:'absolute',left:portrait?84:100,bottom:portrait?105:42,color:C.muted,fontSize:portrait?24:22,letterSpacing:.3}}>合成配音 · 真实界面与功能示意</div>
  <div style={{position:'absolute',right:portrait?84:100,bottom:portrait?105:42,color:C.muted,fontSize:portrait?24:22}}>OPEN SOURCE</div>
  <div style={{position:'absolute',height:3,left:0,bottom:0,width:`${interpolate(f,[0,data.frames],[0,100])}%`,background:C.teal}}/>
 </AbsoluteFill>
};

export const Poster=()=>{
 const {width,height}=useVideoConfig();const p=height>width;
 return <AbsoluteFill style={{background:C.bg,color:C.ink,fontFamily:font,padding:p?'130px 84px':'90px 100px'}}><Grid/><div style={{display:'flex',alignItems:'center',gap:22,zIndex:1}}><Mark size={80}/><div style={{fontSize:48,fontWeight:700}}>Superscience</div></div><div style={{zIndex:1,marginTop:p?170:95}}><Mono style={{color:C.gold,fontSize:30}}>LOCAL RESEARCH WORKBENCH</Mono><h1 style={{fontSize:p?112:135,lineHeight:1.15,letterSpacing:-5,margin:'35px 0'}}>答案之外，<br/>还要有证据。</h1><div style={{fontSize:38,color:C.muted,marginTop:40,lineHeight:1.7}}>文件 · 终端 · Notebook<br/>科学检索 · 产物 · 来源追溯</div></div><div style={{position:'absolute',right:p?150:80,top:p?900:270}}><EvidenceOrbit portrait={p} settled/></div><div style={{position:'absolute',left:p?84:100,bottom:p?160:100,color:C.teal,fontSize:p?34:36,fontFamily:'"SF Mono", Menlo, monospace'}}>github.com/eust-w/superscience</div><div style={{position:'absolute',right:p?84:100,bottom:p?110:50,color:C.muted,fontSize:25}}>APACHE 2.0 · 中文演示</div></AbsoluteFill>
}

// Authored cover layouts keep every label inside the platform's full aspect ratio.
export const SocialPoster=()=>{
 const {width,height}=useVideoConfig();const portrait=height>width;
 return <AbsoluteFill style={{background:C.bg,color:C.ink,fontFamily:font,overflow:'hidden'}}>
  <Grid/>
  <div style={{position:'absolute',left:84,top:84,display:'flex',alignItems:'center',gap:20}}><Mark size={68}/><div style={{fontSize:44,fontWeight:700}}>Superscience</div></div>
  <div style={{position:'absolute',left:84,top:portrait?225:265,width:portrait?912:670}}>
   <Mono style={{color:C.gold,fontSize:portrait?25:24,letterSpacing:1.3}}>LOCAL RESEARCH WORKBENCH</Mono>
   <h1 style={{fontSize:portrait?112:108,lineHeight:1.15,letterSpacing:-4,margin:'32px 0'}}>答案之外，<br/>还要有证据。</h1>
   <div style={{fontSize:portrait?35:34,color:C.muted,lineHeight:1.7,marginTop:28}}>文件 · 终端 · Notebook<br/>科学检索 · 产物 · 来源追溯</div>
  </div>
  <div style={{position:'absolute',left:portrait?210:800,top:portrait?690:280,scale:portrait?.9:.72,transformOrigin:'top left'}}><EvidenceOrbit portrait={portrait} settled/></div>
  <div style={{position:'absolute',left:84,bottom:portrait?150:128,color:C.teal,fontSize:portrait?30:34,fontFamily:'"SF Mono", Menlo, monospace'}}>github.com/eust-w/superscience</div>
  <div style={{position:'absolute',left:84,bottom:portrait?100:80,color:C.muted,fontSize:24,letterSpacing:1}}>APACHE 2.0 · 中文演示</div>
 </AbsoluteFill>
}
