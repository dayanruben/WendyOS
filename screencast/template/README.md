# Feature screencast template

This is a neutral scene-first starting point for a feature-branch screencast.

Each scene lives in one folder:

```text
scenes/
  01-intro/
    slide.md        # Slidev Markdown for this scene
    voice.md   # narration source for this scene
  03-demo/
    slide.md
    voice.md
    vhs.tape       # optional VHS source
    video.mp4       # generated media; ignored by git
```

The scene folder name is the scene id. Scene order is lexicographic, so prefix
folders with numbers: `01-intro`, `02-problem`, `03-demo`, and so on.

Generated files such as `video.mp4` and `voice.mp3` should stay beside the
scene source but remain untracked.

Visual source priority:

1. `video.mp4`, `video.webm`, or `video.gif` if present.
2. `vhs.tape` as the source to render a video. Run `scripts/render-tapes.sh`
   before final rendering.
3. `slide.md` as a still slide when there is no video and no VHS tape.

## Agent prompt

> Read `screencast/README.md`. Use `screencast/template/` as the starting point
> and create a scene-first, deck-first screencast for this feature branch. Keep
> generated media out of git. Use hooks for stateful setup/teardown. Make the
> deck manually presentable and renderable.

## Human workflow

1. Create or edit scene folders under `scenes/`.
2. Put one scene's slide, narration, optional tape, and optional generated media
   in that scene folder. Do not add separate metadata unless the framework grows
   a real need for it.
3. Put project-specific setup in `hooks/setup.sh` and cleanup in
   `hooks/teardown.sh`.
4. From the parent `screencast/` directory, rebuild the aggregate Slidev deck:

   ```sh
   scripts/build-scenes.mjs template
   ```

5. Dry-run tape rendering before executing real commands:

   ```sh
   scripts/render-tapes.sh --dry-run --with-hooks template/scenes/03-demo/vhs.tape
   ```

6. Generate audio and render the final deck after copying/adapting the template
   into the root screencast directory. The renderer derives scene order,
   voiceover, and media from the scene folders; no `timeline.json` is needed for
   scene-first screencasts.
