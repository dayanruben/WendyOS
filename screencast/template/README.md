# Screencast template

Reusable workflow for producing engineering screencasts with terminal clips,
optional UI/docs clips, generated voiceover, and a deterministic final render.

## Asset model

- `tapes/*.tape` — VHS terminal recordings.
- `recordings/` — generated or manually captured video clips (`.mp4`, `.mov`).
- `voiceover/text/*.txt` — voiceover scripts, one file per narrated scene.
- `voiceover/mp3/*.mp3` — generated TTS audio. Ignored by git.
- `scene-plan.tsv` — ordered scene list used by `scripts/render.sh`.
- `stitch/output/` — final rendered video. Ignored by git.

Generated media is intentionally not committed. Keep the scripts, scene plan,
title card, text scripts, and tape sources in git; regenerate clips when needed.

## Standard format

Use the display's logical aspect ratio, with encoder-safe even dimensions:

```text
1728 × 1118, 10 fps
```

For VHS clips, use:

```tape
Set Width 1728
Set Height 1118
Set FontSize 20
Set Framerate 10
```

The height is `1118` instead of `1117` because H.264 requires even dimensions.

## Scene plan

`scene-plan.tsv` columns:

```text
scene_id    video_path    duration_seconds    voiceover_path
```

Paths are relative to this template/project directory. Leave `voiceover_path`
empty for silent scenes.

Duration rule:

```text
scene duration = max(minimum useful video duration, voiceover duration)
```

If voiceover is shorter than the scene duration, the renderer pads with silence.
If voiceover is longer than the minimum video duration, extend the video scene.

## Generate voiceover

Requires `OPENAI_API_KEY`.

```sh
scripts/generate-tts.sh
```

This reads `voiceover/text/*.txt` and writes matching `.mp3` files under
`voiceover/mp3/`.

## Record docs page

```sh
scripts/record-docs-page.mjs \
  https://docs.example.com/page \
  recordings/10-docs-page.mp4
```

## Render final video

```sh
scripts/render.sh
```

Output:

```text
stitch/output/screencast.mp4
```

## Validation

Check all videos:

```sh
for f in recordings/*.{mp4,mov}; do
  [ -e "$f" ] || continue
  ffprobe -v error -select_streams v:0 \
    -show_entries stream=width,height,r_frame_rate,duration \
    -of csv=p=0 "$f"
done
```

Use the generated duration report to confirm no voiceover overruns its scene.
