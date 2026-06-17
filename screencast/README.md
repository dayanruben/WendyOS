# Screencast production

This directory contains reusable tooling for building engineering screencasts.
Start by copying `template/` to a project-specific folder, then edit the scene
plan, title card, VHS tapes, and voiceover text.

```sh
cp -R screencast/template screencast/my-screencast
```

The template intentionally excludes generated media from git. Keep source assets
and scripts versioned; regenerate recordings, TTS audio, and final renders as
needed.
