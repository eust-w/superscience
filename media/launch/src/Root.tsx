import React from 'react';
import {Composition,Still} from 'remotion';
import {Launch} from './Launch';
import {Poster,SocialPoster} from './Design';
export const Root=()=> <>
<Composition id="Landscape" component={Launch} width={1920} height={1080} fps={30} durationInFrames={2418}/>
<Composition id="Portrait" component={Launch} width={1080} height={1920} fps={30} durationInFrames={2418}/>
<Still id="PosterLandscape" component={Poster} width={1920} height={1080}/>
<Still id="PosterPortrait" component={Poster} width={1080} height={1920}/>
<Still id="PosterThreeFour" component={SocialPoster} width={1080} height={1440}/>
<Still id="PosterFourThree" component={SocialPoster} width={1440} height={1080}/>
</>;
