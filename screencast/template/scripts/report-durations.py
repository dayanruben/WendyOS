#!/usr/bin/env python3
import subprocess
import sys
from pathlib import Path


def duration(path: Path) -> float:
    return float(
        subprocess.check_output(
            [
                "ffprobe",
                "-v",
                "error",
                "-show_entries",
                "format=duration",
                "-of",
                "default=nk=1:nw=1",
                str(path),
            ]
        )
        .decode()
        .strip()
    )


def video_stream(path: Path) -> str:
    return (
        subprocess.check_output(
            [
                "ffprobe",
                "-v",
                "error",
                "-select_streams",
                "v:0",
                "-show_entries",
                "stream=width,height,r_frame_rate",
                "-of",
                "csv=p=0",
                str(path),
            ]
        )
        .decode()
        .strip()
    )


def main() -> int:
    plan = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("scene-plan.tsv")
    project_dir = plan.resolve().parent

    print(
        "scene\tvideo\tplanned_seconds\tactual_video_seconds\tvoiceover"
        "\tvoiceover_seconds\tvideo_minus_voiceover_seconds"
        "\tvideo_shorter_than_voiceover\tvideo_stream"
    )

    for line in plan.read_text().splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        parts = line.split("\t")
        if len(parts) < 3:
            raise SystemExit(f"invalid scene-plan row: {line!r}")
        scene, video_rel, planned = parts[:3]
        voice_rel = parts[3] if len(parts) > 3 else ""
        video = project_dir / video_rel
        voice = project_dir / voice_rel if voice_rel else None

        planned_seconds = float(planned)
        actual_video = duration(video) if video.exists() else None
        voice_seconds = duration(voice) if voice and voice.exists() else None
        delta = planned_seconds - voice_seconds if voice_seconds is not None else None
        shorter = voice_seconds is not None and planned_seconds + 0.01 < voice_seconds
        stream = video_stream(video) if video.exists() else ""

        print(
            "\t".join(
                [
                    scene,
                    video_rel,
                    f"{planned_seconds:.3f}",
                    "" if actual_video is None else f"{actual_video:.3f}",
                    voice_rel,
                    "" if voice_seconds is None else f"{voice_seconds:.3f}",
                    "" if delta is None else f"{delta:.3f}",
                    str(shorter).lower(),
                    stream,
                ]
            )
        )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
