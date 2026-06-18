#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TEXT_DIR="$PROJECT_DIR/voiceover/text"
OUT_DIR="$PROJECT_DIR/voiceover/mp3"

MODEL="${OPENAI_TTS_MODEL:-gpt-4o-mini-tts}"
VOICE="${OPENAI_TTS_VOICE:-alloy}"
INSTRUCTIONS="${OPENAI_TTS_INSTRUCTIONS:-Professional technical screencast narration. Calm, confident, concise, neutral English. Avoid hype; sound like an experienced engineer explaining status and tradeoffs.}"
DRY_RUN=0

usage() {
  cat <<'EOF'
usage: generate-tts.sh [--dry-run]

Generates MP3 narration from both supported layouts:

  voiceover/text/*.txt        -> voiceover/mp3/*.mp3
  scenes/*/voice.md          -> scenes/*/voice.mp3

Requires OPENAI_API_KEY unless --dry-run is used. There is intentionally no
local fallback voice generator.
Set OPENAI_TTS_MODEL, OPENAI_TTS_VOICE, or OPENAI_TTS_INSTRUCTIONS to override
TTS defaults.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ "$DRY_RUN" -eq 0 && -z "${OPENAI_API_KEY:-}" ]]; then
  echo "error: OPENAI_API_KEY is required; no local TTS fallback is supported" >&2
  exit 1
fi

shopt -s nullglob
inputs=()
for txt in "$TEXT_DIR"/*.txt; do
  [[ -e "$txt" ]] && inputs+=("$txt")
done
for txt in "$PROJECT_DIR"/scenes/*/voice.md; do
  [[ -e "$txt" ]] && inputs+=("$txt")
done

if [[ "${#inputs[@]}" -eq 0 ]]; then
  echo "warning: no voiceover text files found" >&2
  exit 0
fi

output_for() {
  local txt="$1"
  case "$txt" in
    "$TEXT_DIR"/*.txt)
      mkdir -p "$OUT_DIR"
      local base
      base="$(basename "$txt" .txt)"
      printf '%s/%s.mp3\n' "$OUT_DIR" "$base"
      ;;
    */scenes/*/voice.md)
      printf '%s/voice.mp3\n' "$(dirname "$txt")"
      ;;
    *)
      echo "error: unsupported voiceover source: $txt" >&2
      return 1
      ;;
  esac
}

for txt in "${inputs[@]}"; do
  out="$(output_for "$txt")"
  estimate="$(python3 - "$txt" <<'PY'
import re
import sys
from pathlib import Path
text = Path(sys.argv[1]).read_text(encoding='utf-8').strip()
words = len(re.findall(r"\b[\w'-]+\b", text))
seconds = max(1.0, words / 155 * 60)
print(f"{words} words, ~{seconds:.1f}s")
PY
)"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "would generate $out ($estimate)"
    continue
  fi

  payload="$(mktemp)"
  python3 - "$txt" "$MODEL" "$VOICE" "$INSTRUCTIONS" > "$payload" <<'PY'
import json, sys
text_path, model, voice, instructions = sys.argv[1:]
text = open(text_path, encoding='utf-8').read().strip()
print(json.dumps({
    "model": model,
    "voice": voice,
    "input": text,
    "response_format": "mp3",
    "instructions": instructions,
}))
PY

  tmp="$out.tmp"
  code="$(curl -sS -o "$tmp" -w "%{http_code}" https://api.openai.com/v1/audio/speech \
    -H "Authorization: Bearer $OPENAI_API_KEY" \
    -H "Content-Type: application/json" \
    --data-binary "@$payload")"
  rm -f "$payload"

  if [[ "$code" != "200" ]]; then
    echo "error: TTS failed for $txt (HTTP $code)" >&2
    cat "$tmp" >&2 || true
    rm -f "$tmp"
    exit 1
  fi

  mv "$tmp" "$out"
  duration="$(ffprobe -v error -show_entries format=duration -of default=nk=1:nw=1 "$out" 2>/dev/null || true)"
  echo "generated $out (${estimate}${duration:+, actual $duration s})"
done
