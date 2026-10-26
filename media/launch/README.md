# Superscience launch film

An original Chinese narrated introduction, with separate 16:9 and 9:16 layouts.
The footage uses genuine product screenshots in `docs/readme/` (onboarding,
Notebook, scientific search and artifacts);
other diagrams explain the implemented workflow and are labelled as illustrations.
The film does not claim that all registered connectors or external model providers
have passed live acceptance.

## Rebuild

Requirements: Node.js 24+, npm, macOS `say` with the Tingting voice, FFmpeg and
FFprobe. The source uses system CJK fonts (PingFang SC on macOS); render elsewhere
with a compatible CJK font installed.

```sh
cd media/launch
npm ci
npm run prepare:assets
npm run studio
npm run typecheck
npm run render:landscape
npm run render:portrait
npm run render:posters
npm run render:social-covers
npm run verify:exports
```

`prepare:assets` synthesizes the narration sentence by sentence, measures PCM
sample counts, generates synchronized caption timings and SRT, copies genuine
screenshots, and writes explicit editable scene nodes. It never reads provider
credentials. The exported MP4 files are written to `.qa/video/` at the repository
root; generated media, copied screenshots and `node_modules` are excluded from Git.
The additional 3:4 and 4:3 social covers use their own safe-area layouts instead of cropping the video posters.

If Chrome is installed at a custom location, pass the Remotion CLI
`--browser-executable` flag. The preview is served at `http://localhost:3107`.

## Editorial boundaries

- The 51 figure describes registered scientific connectors, not 51 independently
  validated live integrations.
- Local tools work without a model. Model configuration is required for AI execution; the onboarding screenshot is
  not evidence of a successful model response.
- Permission approval is not a process sandbox.
- Notebook measurements are synthetic demonstration data, not a measured benchmark.
- This film contains synthetic Chinese narration (macOS Tingting). There is no
  third-party music or cloned human voice.
- Original narration and diagram source follow the repository Apache-2.0 license.
  Remotion and its dependencies retain their own licenses; installing this
  isolated production tool does not change the application runtime dependencies.

## Source map

- `narration.json`: original Chinese script and chapter titles.
- `src/Design.tsx`: original vector diagrams and format-specific layouts.
- `src/Launch.tsx`: explicit scene/audio/caption nodes, generated from timing data.
- `src/timing.json`: sentence timing measured from generated speech.
- `subtitles.zh-CN.srt`: synchronized sidecar subtitles.
- `scripts/prepare_assets.py`: local narration generation and screenshot staging.
- `scripts/generate_timeline.py`: repeatable timeline source generation.
- `scripts/normalize_export.py`: upload-ready limited-range BT.709 H.264 and MP4 fast-start conversion.
- `scripts/verify_exports.py`: encoding, dimension, audio, duration and SHA-256 verification.
- `asset-provenance.json`: exact screenshot source files and hashes.
- `src/Captions.tsx`: original caption renderer driven by measured sentence timing.

The dependency override for Zod selects 4.6.6 to address the oversized constrained
collection validation advisory affecting the pinned Remotion toolchain's 4.5.4.
