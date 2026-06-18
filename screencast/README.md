# Screencast production

Reusable workflow for producing engineering screencasts as interactive Slidev
presentations that can also be rendered into narrated videos.

The presentation deck is the source of truth. Generated media such as VHS clips,
screen recordings, browser captures, voiceover MP3s, and final renders are build
artifacts and should not be committed. The checked-in deck is a concrete Wendy
Agent for Mac Beta example that exercises the basic building blocks.

## Structure

```text
screencast/
  scenes/                  # preferred source layout: one folder per scene
    01-intro/
      slide.md             # Slidev Markdown for this scene
      voice.md        # narration source for this scene
      vhs.tape            # optional VHS source for this scene
      video.mp4            # generated scene media; ignored by git
  deck/
    slides.md              # generated or hand-authored Slidev deck
    style.css              # intentionally empty unless custom styling is needed
    public/                # generated presentation media mirrors; ignored by git
  tapes/                   # legacy/global VHS terminal recordings
  hooks/                   # optional preflight/setup/teardown hooks for tape rendering
  voiceover/               # legacy/global narration layout
  output/                  # final renders/exports; ignored by git
  timeline.json            # legacy/hand-authored playback plan; optional for scene-first decks
  package.json             # Slidev commands and dependencies
  scripts/                 # reusable helper scripts
  template/                # neutral scene-first scaffold for new feature screencasts
```

## Presentation engine

Use **Slidev** as the authoring layer.

Why Slidev:

- Markdown-first decks are easy for humans and AI agents to edit.
- It is designed for developer talks: code, themes, components, presenter mode,
  and embedded media are first-class.
- The same `deck/slides.md` can be presented manually or driven by automation.
- Videos from VHS, screen recordings, browsing, and b-roll can be embedded as
  normal slide media.

Install dependencies:

```sh
cd screencast
npm install
```

Present manually:

```sh
npm run present
```

Develop with the local Slidev server:

```sh
npm run dev
```

Render the synchronized narrated video from scene folders or, for legacy decks,
from `timeline.json`:

```sh
scripts/render-deck.mjs
```

## Starting a new feature screencast

This directory is both a working screencast and a reusable framework. For a new
feature branch, start from the neutral scaffold in `template/` and keep the
shared framework files in `scripts/` and `package.json`.

Agent prompt:

```text
Read screencast/README.md. Create a deck-first screencast for this feature
branch. Use screencast/template/ as the starting point. Keep generated media out
of git. Use hooks for any setup/teardown. The result must be manually
presentable with Slidev and renderable with the scene renderer.
```

Human workflow:

1. Copy or adapt `template/scenes/` into `scenes/`. Keep one scene's slide,
   narration, tape, and generated media in that scene folder. The folder name is
   the scene id.
2. Rebuild the aggregate Slidev deck from scenes:

   ```sh
   scripts/build-scenes.mjs
   ```

3. Present manually while drafting:

   ```sh
   npm run present
   ```

4. Check shell/JSON/JS syntax:

   ```sh
   scripts/check.sh
   ```

5. Dry-run tape rendering before executing real commands:

   ```sh
   scripts/render-tapes.sh --dry-run --with-hooks
   ```

6. Render tapes only after reviewing the commands they execute:

   ```sh
   scripts/render-tapes.sh --with-hooks
   scripts/build-scenes.mjs
   ```

7. Generate voiceover and render the final video:

   ```sh
   scripts/generate-tts.sh
   scripts/render-deck.mjs
   ```

## Standard format

Use a 16:10 canvas close to a non-Retina MacBook-sized presentation:

```text
1440 × 900, 10 fps
```

For VHS clips, use the same terminal style as the checked-in tapes:

```tape
Set Shell zsh
Set Width 1440
Set Height 900
Set FontSize 20
Set FontFamily "JetBrains Mono, JetBrainsMono, JetBrainsMono Nerd Font, JetBrainsMono Nerd Font Mono, monospace"
Set Framerate 10
Set CursorBlink false
```

Use `zsh` and avoid switching tapes to `bash` unless the screencast explicitly
calls for it.

The deck uses the default Slidev theme and fonts. VHS clips use the default VHS
terminal theme and JetBrains Mono when available. Disable cursor blinking for
terminal clips so a padded/frozen final frame does not look like a stuck blink
state.

## Building blocks

A screencast is assembled from ordered presentation beats. Prefer one folder per
scene:

```text
scenes/03-demo/
  slide.md
  voice.md
  vhs.tape      # optional
  video.mp4      # generated, ignored
```

The scene folder name is the scene id. Scene order is lexicographic; use numeric
prefixes such as `01-intro`, `02-problem`, and `03-demo`.

Run `scripts/build-scenes.mjs` after editing scene folders. It writes the
aggregate `deck/slides.md` and mirrors existing scene media into
`deck/public/scenes/` for manual Slidev presentation. The renderer derives scene
order, voiceover, and media from the scene folders; no `timeline.json` is needed
for scene-first screencasts.

Visual source priority is intentionally conventional:

1. If the scene has `video.mp4`, `video.webm`, or `video.gif`, that rendered
   video is used.
2. Otherwise, if the scene has `vhs.tape`, the scene expects a generated video;
   run `scripts/render-tapes.sh` first. The renderer fails rather than silently
   falling back to the slide.
3. Otherwise, the scene's `slide.md` is rendered as a still slide.

Useful building blocks include:

- **Slides/cards** — normal Slidev slides for intros, section breaks, diagrams,
  summary points, and closing/contact details.
- **VHS terminal tapes** — deterministic terminal recordings from `tapes/*.tape`.
  Render these to `deck/public/videos/` and embed them in the deck.
- **Screen recordings** — captured application, desktop, or device UI footage.
  Use these when the real interface matters more than deterministic replay.
  Keep raw recordings outside git, transcode them into the relevant scene folder,
  and embed the generated MP4 from that scene's `slide.md`.
- **Live coding** — editor-centric footage where code is written, refactored, or
  debugged in real time.
- **Scripted code reveal** — preferred over ad-hoc live coding when possible.
  Reveal code progressively from patches, before/after files, or curated
  snippets in an editor-like slide. VS Code styling is a good visual target, but
  implementation details can come later.
- **Browsing** — website or web-app navigation captured as a user would
  experience it: product pages, docs exploration, dashboards, account flows, and
  hosted demos.
- **Scripted browser/docs captures** — reproducible browser recordings for docs
  pages, dashboards, changelogs, or web apps.
- **B-roll footage** — supplemental real-world or atmospheric footage, such as
  devices, hardware, people working, lab/office shots, or product context.
- **Overlays** — callouts, captions, arrows, highlights, lower thirds, or labels
  implemented as slide content/fragments or composited later.
- **Audio-only beats** — silence, room tone, music stings, or narration-only
  pauses used to control pacing.

## VHS in the deck model

Keep VHS as a deterministic terminal-video generator:

1. Write or update a scene-local tape such as `scenes/03-demo/vhs.tape`.
2. Make the tape output scene-local generated media, for example
   `Output scenes/03-demo/video.mp4`.
3. Embed the public mirror in `scenes/03-demo/slide.md`:

   ```html
   <video :src="'/scenes/03-demo/video.mp4'" controls muted width="100%"></video>
   ```

4. Run `scripts/build-scenes.mjs` after rendering to mirror the generated video
   into `deck/public/scenes/` for manual presentation.

The older `tapes/` directory is still supported for existing screencasts, but
new feature screencasts should prefer scene-local tapes.

In Slidev Markdown, use a Vue-bound `src` for files under `deck/public/` so Vite
will serve the asset without trying to import it from the filesystem:

```html
<video :src="'/videos/mac-beta/01-install-launch.mp4'" controls muted width="100%"></video>
```

Render checked-in tapes through the hardened wrapper instead of invoking `vhs`
directly:

```sh
scripts/render-tapes.sh --dry-run
scripts/render-tapes.sh
```

For stateful screencasts, add project hooks under `hooks/` and render with:

```sh
scripts/render-tapes.sh --with-hooks
```

The hook contract is intentionally small:

- `hooks/preflight.sh` runs non-destructive checks by default.
- `hooks/setup.sh` runs only when requested, for destructive setup/reset work.
- `hooks/teardown.sh` runs only when requested, for cleanup after rendering.
- Hooks receive `SCREENCAST_ROOT`, `SCREENCAST_YES`, and `SCREENCAST_DRY_RUN`.

Hooks are project-local: keep `scripts/render-tapes.sh` generic, and put any
screencast-specific reset or cleanup logic in `hooks/*.sh`. A template can ship
empty/no-op hooks; a concrete screencast can replace them with target-specific
setup.

This keeps terminal demos repeatable while making them ordinary presentation
media. Later, terminal playback could move to asciinema or xterm.js without
changing the high-level deck/timeline model.

## Scene timing and voiceover

For scene-first screencasts, timing is derived from files in each scene folder:

- folder name -> scene id
- sorted folder order -> slide order
- `voice.mp3` -> narration duration
- `video.mp4`, `video.webm`, or `video.gif` -> media duration

The renderer uses this rule:

```text
scene duration = max(voiceover duration, media duration)
```

If the voiceover is longer than the video, the video freezes on the last frame.
If the video is longer than the voiceover, the audio is extended with silence.
Silent slide-only scenes use a small fallback duration, configurable with
`SCREENCAST_DEFAULT_SCENE_SECONDS`.

Existing hand-authored `timeline.json` files are still supported for older decks,
but new feature screencasts should not need one.

## Generate voiceover

Requires `OPENAI_API_KEY`. The workflow intentionally fails hard when the key is
missing; do not use local fallback voices such as `say` because inconsistent
narration quality makes renders harder to review and reproduce.

```sh
scripts/generate-tts.sh --dry-run
scripts/generate-tts.sh
```

This reads both scene-local `scenes/*/voice.md` files and the legacy
`voiceover/text/*.txt` layout. Scene-local audio is written as
`scenes/*/voice.mp3`. Dry-run mode prints word counts and rough duration
estimates without calling the API or requiring an API key.

## Record screen/docs/browser footage

Raw UI recordings are disposable source media and should stay outside git. Convert
or trim them into the deck media directory before rendering:

```sh
ffmpeg -i /path/to/screen-recording.mov \
  -vf 'scale=1440:900:force_original_aspect_ratio=decrease,pad=1440:900:(ow-iw)/2:(oh-ih)/2:color=black,fps=10,format=yuv420p' \
  -an deck/public/videos/mac-beta/ui-agent-menu-permissions.mp4
```

For scripted browser/docs captures:

```sh
scripts/record-docs-page.mjs \
  https://docs.example.com/page \
  deck/public/videos/03-docs-example.mp4
```

## AI-assisted production workflow

A good agent loop is:

1. **Plan** — write a narrative outline with audience, goal, key beats, and call
   to action.
2. **Choose building blocks** — pick slides, VHS, screen recording, browsing,
   scripted code reveal, b-roll, or overlays for each beat.
3. **Draft the deck** — update `deck/slides.md` so the talk can be presented
   manually. Keep `deck/style.css` empty unless custom styling is explicitly
   needed.
4. **Draft sources** — create/update scene folders with slides, VHS tapes,
   capture notes, code-reveal snippets, and voiceover text.
5. **Prepare hooks** — for stateful recordings, put checks in `hooks/preflight.sh`,
   destructive setup in `hooks/setup.sh`, and cleanup in `hooks/teardown.sh`.
6. **Generate media** — render VHS/browser/screen assets into their scene folders,
   then run `scripts/build-scenes.mjs` to refresh the deck.
7. **Generate voiceover** — create scene-local `voice.mp3` files.
8. **Render/review** — produce the final video, watch it, and iterate on the
   smallest source asset that fixes the issue.

The agent should treat generated footage as disposable build output. If a scene
needs correction, change the declarative source and regenerate it rather than
manually editing rendered media.

## Validation

Run syntax and generated-media checks:

```sh
scripts/check.sh
```
