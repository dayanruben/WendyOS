"""Stage and deploy the inference app with its own Wendy config/build context."""
from pathlib import Path
import argparse
import os
import shutil
import subprocess
import tempfile


def deployment_env(entries):
    values = dict(entry.split("=", 1) for entry in entries)
    token = values.get("COKE_HIL_TOKEN", os.environ.get("COKE_HIL_TOKEN", ""))
    if len(token) < 16 or any(char in token for char in "\r\n"):
        raise ValueError("set COKE_HIL_TOKEN to a secret of at least 16 characters before deploying")
    values["COKE_HIL_TOKEN"] = token
    return [f"{key}={value}" for key, value in values.items()]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cli", default="wendy")
    parser.add_argument("--device", required=True)
    parser.add_argument("--detach", action="store_true")
    parser.add_argument("--env", action="append", default=[])
    parser.add_argument("--build-host")
    args = parser.parse_args()
    try:
        args.env = deployment_env(args.env)
    except ValueError as error:
        parser.error(str(error))
    root = Path(__file__).resolve().parents[1]
    # Resolve relative CLI paths before Wendy changes its build directory.
    cli = str(Path(args.cli).resolve()) if "/" in args.cli else args.cli
    subprocess.run(["python3", str(root / "prepare-assets.py"), "--check"], check=True, cwd=root)
    with tempfile.TemporaryDirectory(prefix="g1-coke-hil-") as temporary:
        stage = Path(temporary)
        for name in ("coke_demo", "runtime", "bundle"):
            shutil.copytree(root / name, stage / name, ignore=shutil.ignore_patterns("__pycache__", "*.pyc"))
        for name in ("wendy.json", "build.stagefile.yaml", "build.stagefile.lock.yaml"):
            if (root / "hil" / name).exists():
                shutil.copy2(root / "hil" / name, stage / name)
        command = [cli, "run", "--device", args.device, "--prefix", str(stage),
                   "--dockerfile", "build.stagefile.yaml"]
        if args.detach:
            command.append("--detach")
        if args.build_host:
            command.extend(["--build-host", args.build_host])
        for value in args.env:
            command.extend(["--env", value])
        try:
            return subprocess.call(command)
        finally:
            lock = stage / "build.stagefile.lock.yaml"
            if lock.exists():
                shutil.copy2(lock, root / "hil" / lock.name)


if __name__ == "__main__":
    raise SystemExit(main())
