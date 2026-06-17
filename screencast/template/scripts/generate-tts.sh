#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TEXT_DIR="$PROJECT_DIR/voiceover/text"
OUT_DIR="$PROJECT_DIR/voiceover/mp3"

MODEL="${OPENAI_TTS_MODEL:-gpt-4o-mini-tts}"
VOICE="${OPENAI_TTS_VOICE:-alloy}"
INSTRUCTIONS="${OPENAI_TTS_INSTRUCTIONS:-Professional technical screencast narration. Calm, confident, concise, neutral English. Avoid hype; sound like an experienced engineer explaining status and tradeoffs.}"

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  echo "error: OPENAI_API_KEY is required" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"

for txt in "$TEXT_DIR"/*.txt; do
  [[ -e "$txt" ]] || continue
  base="$(basename "$txt" .txt)"
  out="$OUT_DIR/$base.mp3"
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
    echo "error: TTS failed for $base (HTTP $code)" >&2
    cat "$tmp" >&2 || true
    rm -f "$tmp"
    exit 1
  fi

  mv "$tmp" "$out"
  duration="$(ffprobe -v error -show_entries format=duration -of default=nk=1:nw=1 "$out" 2>/dev/null || true)"
  echo "generated $out ${duration:+($duration s)}"
done
