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
  deck/
    slides.md              # Slidev deck; manually presentable
    style.css              # intentionally empty unless a project needs custom styling
    public/
      videos/              # generated/recorded footage embedded by slides
      images/              # images and generated stills embedded by slides
  tapes/                   # VHS terminal recordings
  voiceover/
    text/                  # narration scripts, one file per narrated beat
    mp3/                   # generated OpenAI TTS audio; ignored by git
  output/                  # final renders/exports; ignored by git
  timeline.json            # engine-neutral playback/sync plan
  package.json             # Slidev commands and dependencies
  scripts/                 # helper scripts
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

Render the synchronized narrated video from `timeline.json`:

```sh
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

A screencast is assembled from ordered presentation beats. Useful building
blocks include:

- **Slides/cards** — normal Slidev slides for intros, section breaks, diagrams,
  summary points, and closing/contact details.
- **VHS terminal tapes** — deterministic terminal recordings from `tapes/*.tape`.
  Render these to `deck/public/videos/` and embed them in the deck.
- **Screen recordings** — captured application, desktop, or device UI footage.
  Use these when the real interface matters more than deterministic replay.
  Keep raw recordings outside git, transcode them into `deck/public/videos/`, and
  reference the generated MP4 from `timeline.json`.
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

1. Write or update a tape in `tapes/`.
2. Render it with `vhs`.
3. Put the generated MP4/WebM under `deck/public/videos/`.
4. Embed the video in `deck/slides.md`.
5. Reference the same media file from `timeline.json` for automated playback.

In Slidev Markdown, use a Vue-bound `src` for files under `deck/public/` so Vite
will serve the asset without trying to import it from the filesystem:

```html
<video :src="'/videos/mac-beta/01-install-launch.mp4'" controls muted width="100%"></video>
```

Render all checked-in Mac beta tapes with:

```sh
for tape in tapes/*.tape; do vhs "$tape"; done
```

This keeps terminal demos repeatable while making them ordinary presentation
media. Later, terminal playback could move to asciinema or xterm.js without
changing the high-level deck/timeline model.

## Timeline and voiceover

`timeline.json` maps deck positions to optional voiceover and media assets. The
intended rendering rule remains:

```text
beat duration = max(minimum useful visual duration, voiceover duration)
```

Example timeline step:

```json
{
  "id": "terminal-demo",
  "target": "3",
  "minSeconds": 12.0,
  "voiceover": "voiceover/mp3/02-install-launch.mp3",
  "media": "deck/public/videos/mac-beta/01-install-launch.mp4"
}
```

The `target` value is intentionally engine-neutral. For Slidev it can represent
a slide or future slide/fragment address. A later renderer should open the deck,
navigate to each target, play embedded media as needed, and mux/pad audio so the
beat duration follows the rule above.

## Generate voiceover

Requires `OPENAI_API_KEY`. The workflow intentionally fails hard when the key is
missing; do not use local fallback voices such as `say` because inconsistent
narration quality makes renders harder to review and reproduce.

```sh
scripts/generate-tts.sh --dry-run
scripts/generate-tts.sh
```

This reads `voiceover/text/*.txt` and writes matching `.mp3` files under
`voiceover/mp3/`. Dry-run mode prints word counts and rough duration estimates
without calling the API or requiring an API key.

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
4. **Draft sources** — create/update VHS tapes, capture notes, code-reveal
   snippets, and voiceover text.
5. **Generate media** — render VHS/browser/screen assets into
   `deck/public/videos/` and generate OpenAI TTS into `voiceover/mp3/`.
6. **Update timeline** — map deck targets to voiceover/media in `timeline.json`.
7. **Render/review** — produce the final video, watch it, and iterate on the
   smallest source asset that fixes the issue.

The agent should treat generated footage as disposable build output. If a scene
needs correction, change the declarative source and regenerate it rather than
manually editing rendered media.

## Validation

Run syntax and generated-media checks:

```sh
scripts/check.sh
```
