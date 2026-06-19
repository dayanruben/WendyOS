# Screencast production

KISS workflow for producing narrated engineering screencasts from one creative
source file plus generated scene folders.

`script.md` is the source of truth for **what to show and say**. This README is
the source of truth for **how to turn that script into renderable scenes**.

Generated artifacts keep the source filename and add the output extension:

```text
slide.md   -> slide.md.mp4
vhs.tape   -> vhs.tape.mp4
voice.md   -> voice.md.mp3
blah.vhs   -> blah.vhs.mp4
```

Generated media and final renders are build outputs and should not be committed.

## Structure

```text
screencast/
  script.md                # content + intent: what to show and say
  scenes/                  # agent-maintained renderable scene files
    01-intro/
      slide.md             # Slidev Markdown still frame source
      voice.md             # narration source
      voice.md.mp3         # generated narration; ignored by git
      slide.md.mp4         # generated still-slide movie; ignored by git
    02-demo/
      slide.md
      voice.md
      vhs.tape             # optional terminal recording source
      vhs.tape.mp4         # generated terminal movie; ignored by git
  hooks/                   # optional preflight/setup/teardown hooks for tapes
  output/                  # final renders; ignored by git
  scripts/
    render-slide
    render-tape
    render-voice
    stitch
```

## Install

```sh
cd screencast
npm ci
```

Use `npm ci` so installs use the committed lock file without mutating it. When
updating dependencies, pin exact versions in `package.json`, regenerate the lock
file intentionally, and run `npm audit --audit-level=high` before opening a PR.
The `Screencast` GitHub Actions workflow enforces `npm ci`, high-severity audit
checks, dependency sanity checks, and script validation for changes under
`screencast/`.

`render-slide` uses Slidev under the hood to render one `slide.md` at a time.
There is no aggregate deck and no timeline file.

## Core commands

```sh
scripts/render-slide <scene-dir|slide.md|scene-prefix>
scripts/render-tape  <scene-dir|tape-file|scene-prefix>
scripts/render-voice <scene-dir|voice.md|scene-prefix>
scripts/stitch [scene-dir ...] [--output output.mp4]
```

Examples:

```sh
scripts/render-slide scenes/01-intro
scripts/render-voice /path/to/screencast/scenes/01-intro/voice.md
scripts/render-tape 01
scripts/render-tape x/y/blah.vhs
scripts/stitch scenes/* --output output/feature-name.mp4
```

A short prefix such as `01` expands to the unique matching folder under
`scenes/`, for example `scenes/01-intro`. Ambiguous prefixes fail.

## Typical workflow

1. Human and agent collaborate on `script.md`. It should describe the story,
   scene intent, what to say, and what to show.
2. The agent updates `scenes/*` to match `script.md`:

   ```text
   scenes/01-intro/slide.md
   scenes/01-intro/voice.md
   scenes/02-demo/vhs.tape
   ```

3. Add `vhs.tape` only for scenes that need terminal automation.
4. Render generated scene artifacts:

   ```sh
   scripts/render-slide 01
   scripts/render-voice 01
   scripts/render-tape --dry-run --with-hooks 02
   scripts/render-tape --with-hooks 02
   ```

5. Stitch everything. Prefer naming the final file as a dasherized slug of the
   title in `script.md`:

   ```sh
   scripts/stitch scenes/* --output output/feature-name.mp4
   ```

`stitch` accepts scene folders in the order they should appear, so you can pass a
shell glob or list scenes individually:

```sh
scripts/stitch scenes/* --output output/feature-name.mp4
scripts/stitch scenes/01-intro scenes/02-demo --output output/feature-name.mp4
```

If no scene folders are provided, `scripts/stitch` reads `scenes/*`. If no output
path is provided, the final video is written to:

```text
output/screencast.mp4
```

Agents should prefer passing an explicit output path named as a dasherized slug
of the title in `script.md`; for example `# Wendy File Sync` should use:

```sh
scripts/stitch scenes/* --output output/wendy-file-sync.mp4
```

## Script format

`script.md` is not a build file. Do not put workflow instructions there. Keep it
focused on content and intent: audience, goal, what to say, what to show, and any
demo beats.

Use one `##` heading per scene. Inside each scene, use `### Say` for narration
and `### Show (<role>)` for visual direction. Common show roles are `slide`,
`terminal`, `UI`, `screen recording`, `code`, and `diagram`.

````md
# Feature Name

Audience: Edge app developers.
Goal: Explain the feature and the workflow it enables.
Tone: Calm, technical, direct.

---

## 01 Problem

### Say

Narration for this scene goes here.

### Show (diagram)

A simple diagram or slide idea goes here.

---

## 02 Developer Flow

### Say

Narration for the demo goes here.

### Show (terminal)

```sh
wendy run --device lab-edge-01
```
````

The agent should derive `scenes/*/slide.md`, `voice.md`, and optional `vhs.tape`
from this script while preserving the script as the canonical content source.

## Format

Default output format:

```text
1440 × 900, 10 fps
```

Override with environment variables:

```sh
SCREENCAST_WIDTH=1440 SCREENCAST_HEIGHT=900 SCREENCAST_FPS=10 scripts/stitch scenes/* --output output/feature-name.mp4
```

For VHS clips, use:

```tape
Set Shell zsh
Set Width 1440
Set Height 900
Set FontSize 20
Set FontFamily "JetBrains Mono, JetBrainsMono, JetBrainsMono Nerd Font, JetBrainsMono Nerd Font Mono, monospace"
Set Framerate 10
Set CursorBlink false
```

## Visual source priority

For each scene, `scripts/stitch` chooses one visual source:

1. `video.mp4`, `video.webm`, or `video.gif` if present.
2. Exactly one other scene-local `.mp4`, `.webm`, or `.gif`, such as
   `screen-recording.mov.mp4`.
3. `vhs.tape.mp4` when `vhs.tape` exists.
4. `slide.md.mp4` as the still-slide fallback.

If a required generated artifact is missing, `stitch` fails with the command to
run, such as `scripts/render-slide 01-intro` or `scripts/render-voice 01-intro`.

## Timing

For each scene:

```text
scene duration = max(voice duration, visual media duration)
```

If narration is longer than video, the final frame is frozen. If video is longer
than narration, audio is padded with silence. Silent scenes use
`SCREENCAST_DEFAULT_SCENE_SECONDS` when both media and voice durations are zero.

## Voiceover

`render-voice` requires `OPENAI_API_KEY`. There is intentionally no local voice
fallback. Treat this as a secret: prefer a secrets manager such as 1Password CLI,
macOS Keychain, or a CI secret variable; do not hard-code it in scripts or commit
it in `.env` files. Local `.env` files are ignored by git, but should still be
kept out of shared artifacts and rotated if exposed. Copy `.env.example` for the
supported variable names, and leave `OPENAI_API_KEY` empty until you inject it
from a local secrets manager or CI secret.

```sh
scripts/render-voice --dry-run 01
scripts/render-voice 01
```

Optional overrides:

```sh
OPENAI_TTS_MODEL=gpt-4o-mini-tts \
OPENAI_TTS_VOICE=alloy \
OPENAI_TTS_SPEED=1.2 \
scripts/render-voice 01
```

`OPENAI_TTS_SPEED` accepts `0.25` through `4.0`; the default is `1.25` for a
slightly tighter screencast pace.

## Tape hooks

`tape` rendering can execute real local and device commands, so use dry-run first:

```sh
scripts/render-tape --dry-run --with-hooks 02
scripts/render-tape --with-hooks 02
```

Hook contract:

- `hooks/preflight.sh` runs non-destructive checks by default.
- `hooks/setup.sh` runs only with `--setup` or `--with-hooks`.
- `hooks/teardown.sh` runs only with `--teardown` or `--with-hooks`.
- Hooks receive `SCREENCAST_ROOT`, `SCREENCAST_YES`, and `SCREENCAST_DRY_RUN`.
- Review hook scripts before running them; they execute with your local user
  privileges and may also call connected devices.
- Hooks are refused in CI by default. Use `--force-ci-hooks --yes` only in a
  reviewed workflow that has explicit human approval or runs in a sandboxed
  container, VM, or restricted user account.

## Screen/browser footage

Keep raw recordings outside git. Transcode or capture into the relevant scene
folder and append `.mp4` to the source name:

```sh
ffmpeg -i /path/to/screen-recording.mov \
  -vf 'scale=1440:900:force_original_aspect_ratio=decrease,pad=1440:900:(ow-iw)/2:(oh-ih)/2:color=black,fps=10,format=yuv420p' \
  -an scenes/04-ui/screen-recording.mov.mp4
```

For scripted browser captures:

```sh
scripts/record-page.mjs https://docs.example.com/page scenes/03-docs/page.capture.mp4
```

`record-page.mjs` only opens public `https:` URLs by default and rejects
localhost, private, link-local, and metadata-service addresses. For trusted local
captures, pass `--allow-unsafe-urls` explicitly. The flag is refused in CI, and
`SCREENCAST_ALLOW_UNSAFE_URLS` is obsolete; `scripts/check.sh` fails if that
environment variable is present.

## Operational logging

The scripts write render progress and command metadata to stdout/stderr. Keep CI
logs for at least 90 days when this workflow is run in automation, and archive
`output/duration-report.tsv` with final renders when you need scene timing
provenance. `render-voice` logs model, voice, scene output, and duration
metadata, but not the narration text or API key. For audit-sensitive renders,
pipe command output to a retained JSONL or CI log artifact owned by the workflow
runner rather than committing generated logs to git.

## Validation

```sh
scripts/check.sh
```
