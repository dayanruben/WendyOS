#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SVG="${1:-$PROJECT_DIR/title-card.svg}"
PNG="${2:-$PROJECT_DIR/title-card.png}"
MP4="${3:-$PROJECT_DIR/recordings/00-title-card.mp4}"
DURATION="${TITLE_CARD_SECONDS:-2}"
FPS="${SCREENCAST_FPS:-10}"

mkdir -p "$(dirname "$MP4")"

# macOS `sips` preserves the SVG canvas size. If you use another platform,
# replace this with rsvg-convert, ImageMagick, or a browser screenshot step.
sips -s format png "$SVG" --out "$PNG" >/dev/null

ffmpeg -nostdin -y \
  -loop 1 -i "$PNG" \
  -f lavfi -i anullsrc=channel_layout=stereo:sample_rate=48000 \
  -t "$DURATION" \
  -vf "fps=$FPS,format=yuv420p" \
  -c:v libx264 -preset medium -crf 18 \
  -c:a aac -b:a 192k \
  -shortest -movflags +faststart \
  "$MP4"

echo "wrote $MP4"
