#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SCENE_PLAN="${1:-$PROJECT_DIR/scene-plan.tsv}"
OUT_DIR="$PROJECT_DIR/stitch/output"
BUILD_DIR="$PROJECT_DIR/stitch/build"
OUT_FILE="${OUT_FILE:-$OUT_DIR/screencast.mp4}"
WIDTH="${SCREENCAST_WIDTH:-1728}"
HEIGHT="${SCREENCAST_HEIGHT:-1118}"
FPS="${SCREENCAST_FPS:-10}"
CRF="${SCREENCAST_CRF:-18}"

mkdir -p "$OUT_DIR"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR/video" "$BUILD_DIR/audio"

if [[ ! -f "$SCENE_PLAN" ]]; then
  echo "error: scene plan not found: $SCENE_PLAN" >&2
  exit 1
fi

while IFS=$'\t' read -r scene video_rel duration voiceover_rel rest; do
  [[ -z "${scene:-}" || "${scene:0:1}" == "#" ]] && continue
  video="$PROJECT_DIR/$video_rel"
  if [[ ! -f "$video" ]]; then
    echo "error: scene $scene video not found: $video" >&2
    exit 1
  fi

  echo "Rendering scene $scene video (${duration}s)"
  ffmpeg -nostdin -y -i "$video" -t "$duration" \
    -vf "fps=$FPS,scale=$WIDTH:$HEIGHT:flags=lanczos,setsar=1,format=yuv420p" \
    -an -c:v libx264 -preset medium -crf "$CRF" -pix_fmt yuv420p -movflags +faststart \
    "$BUILD_DIR/video/$scene.mp4" >/tmp/screencast-render-video.log 2>&1

  if [[ -n "${voiceover_rel:-}" ]]; then
    voiceover="$PROJECT_DIR/$voiceover_rel"
    if [[ ! -f "$voiceover" ]]; then
      echo "error: scene $scene voiceover not found: $voiceover" >&2
      exit 1
    fi
    echo "Rendering scene $scene audio with VO"
    ffmpeg -nostdin -y -i "$voiceover" -t "$duration" \
      -af "apad=pad_dur=$duration,atrim=0:$duration,asetpts=PTS-STARTPTS" \
      -ar 48000 -ac 2 -c:a pcm_s16le "$BUILD_DIR/audio/$scene.wav" >/tmp/screencast-render-audio.log 2>&1
  else
    echo "Rendering scene $scene silent audio"
    ffmpeg -nostdin -y -f lavfi -i "anullsrc=channel_layout=stereo:sample_rate=48000" -t "$duration" \
      -c:a pcm_s16le "$BUILD_DIR/audio/$scene.wav" >/tmp/screencast-render-audio.log 2>&1
  fi
done < "$SCENE_PLAN"

: > "$BUILD_DIR/video-list.txt"
for f in "$BUILD_DIR"/video/*.mp4; do
  printf "file '%s'\n" "$(python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "$f")" >> "$BUILD_DIR/video-list.txt"
done
ffmpeg -nostdin -y -f concat -safe 0 -i "$BUILD_DIR/video-list.txt" -c copy "$BUILD_DIR/video-full.mp4" >/tmp/screencast-concat-video.log 2>&1

: > "$BUILD_DIR/audio-list.txt"
for f in "$BUILD_DIR"/audio/*.wav; do
  printf "file '%s'\n" "$(python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "$f")" >> "$BUILD_DIR/audio-list.txt"
done
ffmpeg -nostdin -y -f concat -safe 0 -i "$BUILD_DIR/audio-list.txt" -c:a pcm_s16le "$BUILD_DIR/audio-full.wav" >/tmp/screencast-concat-audio.log 2>&1

ffmpeg -nostdin -y -i "$BUILD_DIR/video-full.mp4" -i "$BUILD_DIR/audio-full.wav" \
  -map 0:v:0 -map 1:a:0 -c:v copy -c:a aac -b:a 192k -shortest -movflags +faststart \
  "$OUT_FILE" >/tmp/screencast-mux-final.log 2>&1

python3 "$SCRIPT_DIR/report-durations.py" "$SCENE_PLAN" > "$PROJECT_DIR/stitch/duration-report.tsv"

echo "wrote $OUT_FILE"
ffprobe -v error -show_entries format=duration -of default=nk=1:nw=1 "$OUT_FILE"
ffprobe -v error -select_streams v:0 -show_entries stream=width,height,r_frame_rate -of csv=p=0 "$OUT_FILE"
