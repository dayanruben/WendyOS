#!/usr/bin/env python3
"""Copy runtime plugin skills into the CLI embed tree; --check detects drift."""
import argparse
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "plugins/wendy-agentic-coding/skills"
TARGET = ROOT / "go/internal/cli/assets/skills"
SKILLS = (
    "wendy-install", "wendy-device-install", "wendy-robot-deploy",
    "wendy-template-app", "wendy-project-setup", "wendy-mcp-setup",
    "wendy-entitlements", "wendy-device-ops", "wendy-device-debug",
    "wendy-app-lifecycle",
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    differences = []
    for skill in SKILLS:
        files = {p.relative_to(SOURCE / skill): p for p in (SOURCE / skill).rglob("*") if p.is_file()}
        if Path("SKILL.md") not in files:
            raise SystemExit(f"Missing source skill: {skill}")
        for relative, source in files.items():
            target = TARGET / skill / relative
            if not target.exists() or target.read_bytes() != source.read_bytes():
                differences.append(str(target.relative_to(ROOT)))
                if not args.check:
                    target.parent.mkdir(parents=True, exist_ok=True)
                    target.write_bytes(source.read_bytes())
        extras = {p.relative_to(TARGET / skill) for p in (TARGET / skill).rglob("*") if p.is_file()} - files.keys()
        if extras:
            raise SystemExit(f"Remove stale embedded files for {skill}: {sorted(map(str, extras))}")
    if args.check and differences:
        raise SystemExit("Run python3 scripts/sync-agent-skills.py:\n" + "\n".join(differences))
    print(f"{'Checked' if args.check else 'Synced'} {len(SKILLS)} runtime skills")


if __name__ == "__main__":
    main()
