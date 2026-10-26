# Launch film validation

Validated 2026-10-26 on macOS. Video duration: 80.619 seconds.

- TypeScript: `npm run typecheck` passed.
- Dependencies: `npm audit --omit=dev` reported zero vulnerabilities. Zod 4.6.6 is intentionally overridden; Remotion warns about its preferred 4.5.4 version. Both full renders completed.
- Eight sample frames from each full export were inspected; portrait feature and Notebook scenes were also inspected at full resolution. Titles and captions fit their safe areas.
- Sentence timings come from actual PCM sample counts. The timeline has 2,418 contiguous frames at 30 fps. Source narration peaks remain below -2.8 dBFS.
- Final exports were normalized to limited-range BT.709 H.264/yuv420p, AAC stereo at 48 kHz, with MP4 fast-start.
- The real UI captures retain their evidence limits: synthetic Notebook data, real Crossref results, local share/replay, and no validated external model response.

| Export | Size | SHA-256 |
| --- | ---: | --- |
| `superscience-launch-1080p.mp4` | 13,833,024 bytes | `c8ef58d6ea38de8ea406b95c01fe4b0cafe7df92494f8da463bf7054e49bb3a0` |
| `superscience-launch-vertical.mp4` | 14,603,560 bytes | `2d954f105e47825a1d4f69b1b9027011089b11a3e0e53f6a8b7f03f236984fe6` |
| `poster-landscape.png` | 188,087 bytes | `7ac61ddb22076dc9956982a60550fb48b13561a3f8037747c056ee0b12bdd093` |
| `poster-portrait.png` | 188,985 bytes | `1a466697e81464fc01f107d85f1775ec310a71cd7316f64900b577f480349fde` |
| `subtitles.zh-CN.srt` | 1,480 bytes | `c428e43f891eea153bdd67125392059eff8313b9e96288bd0f06632fd0b81d1b` |

Exports and QA contact sheets are local build outputs under `.qa/video/`, intentionally excluded from Git. Source screenshots are tracked under `docs/readme/`.
