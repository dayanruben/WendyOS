#!/usr/bin/env python3
"""Opt-in CLI/VM regression: detached deploy -> reported URL -> HTTP proof.

Requires QEMU, a working Docker builder, and a local published VM image.
Creates two disposable generic VMs in a private config directory. Saves command
outputs and HTTP evidence; stops and removes only those VMs on exit.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def free_port_pair():
    for _ in range(100):
        with socket.socket() as first, socket.socket() as second:
            first.bind(("127.0.0.1", 0))
            port = first.getsockname()[1]
            if port == 65535:
                continue
            try:
                second.bind(("127.0.0.1", port + 1))
            except OSError:
                continue
            return port
    raise RuntimeError("could not find two consecutive free agent ports")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--wendy", type=Path, required=True)
    parser.add_argument("--image", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--base-image", default="python:3.12-alpine@sha256:4c47124a8391cb7a9f571164147d154777cf012a4ece5f86097130d7a4478111")
    args = parser.parse_args()
    binary, image = args.wendy.resolve(), args.image.resolve()
    if not binary.is_file() or not image.is_file():
        parser.error("--wendy and --image must be existing files")
    # macOS UNIX sockets have a 104-byte path limit; QMP lives under config/vms.
    root = args.output.resolve() if args.output else Path(tempfile.mkdtemp(prefix="wendy-endpoints-", dir="/tmp"))
    root.mkdir(parents=True, exist_ok=True)
    config = root / "config"
    if config.exists():
        parser.error("output directory must not already contain config (VM isolation)")
    config.mkdir(mode=0o700)
    (root / "metadata.json").write_text(json.dumps({
        "wendy": str(binary), "wendy_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "image": str(image), "base_image": args.base_image,
    }, indent=2) + "\n")
    env = {**os.environ, "WENDY_CONFIG_DIR": str(config), "WENDY_SECRET_STORE": "file",
           "WENDY_ANALYTICS": "false", "CI": "1", "TERM": "dumb"}
    names = ["endpoint-a", "endpoint-b"]
    steps = []

    def cli(label, *argv, expected=0, timeout=240):
        result = subprocess.run([str(binary), *argv], cwd=root, env=env,
                                stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=timeout)
        (root / f"{label}.stdout").write_text(result.stdout)
        (root / f"{label}.stderr").write_text(result.stderr)
        steps.append({"step": label, "args": list(argv), "exit_code": result.returncode})
        if (expected == 0 and result.returncode != 0) or (expected != 0 and result.returncode == 0):
            raise RuntimeError(f"{label}: unexpected exit {result.returncode}: {result.stderr[-2000:]}")
        return result

    def project(name, port):
        path = root / name
        path.mkdir()
        nonce = uuid.uuid4().hex
        app_id = "test.detached." + nonce
        (path / "Dockerfile").write_text(f"FROM {args.base_image}\nWORKDIR /app\nCOPY server.py .\nCMD [\"python3\", \"server.py\"]\n")
        (path / "server.py").write_text(
            "from http.server import BaseHTTPRequestHandler, HTTPServer\n"
            "class Handler(BaseHTTPRequestHandler):\n"
            "    def do_GET(self):\n"
            f"        body = {nonce.encode()!r}\n"
            "        self.send_response(200)\n"
            "        self.send_header('Content-Length', str(len(body)))\n"
            "        self.end_headers()\n"
            "        self.wfile.write(body)\n"
            f"HTTPServer(('0.0.0.0', {port}), Handler).serve_forever()\n")
        (path / "wendy.json").write_text(json.dumps({
            "appId": app_id, "version": "1.0.0", "platform": "linux",
            "entitlements": [{"type": "network", "mode": "host"}, {"type": "http", "port": port}],
            "readiness": {"tcpSocket": {"port": port}, "timeoutSeconds": 25},
        }))
        return path, app_id, nonce

    def deploy(label, name, path, app_id, *extra):
        result = cli(label, "--json", "--device", f"vm:{name}", "run", "--detach", "--yes",
                     "--skip-cloud-registration", "--prefix", str(path), *extra)
        payload = json.loads(result.stdout)  # Exactly one JSON document, no progress/log contamination.
        assert payload["status"] == "started" and payload["readiness"] == "not_checked", payload
        assert payload["app"] == app_id and payload["device"] == f"vm:{name}", payload
        url = payload["url"]
        assert url.startswith("http://127.0.0.1:"), payload
        assert payload["endpoints"] == [{"app": app_id, "url": url}], payload
        return url

    def verify(label, url, nonce):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        deadline = time.monotonic() + 30
        while True:
            try:
                with opener.open(url, timeout=3) as response:
                    body = response.read(4096).decode()
                    assert response.status == 200 and body == nonce, (url, response.status, body)
                steps.append({"step": label, "url": url, "status": 200, "nonce": body})
                return
            except (OSError, urllib.error.URLError):
                if time.monotonic() >= deadline:
                    raise
                time.sleep(0.5)

    try:
        for name in names:
            cli(f"create-{name}", "vm", "create", name, "--image", str(image), "--profile", "generic", "--yes")
            cli(f"start-{name}", "vm", "start", name, "--detach", "--port", str(free_port_pair()), "--yes")
            cli(f"info-{name}", "--json", "--device", f"vm:{name}", "device", "info")

        port_a, port_b = free_port_pair(), free_port_pair()
        while port_b in (port_a, port_a + 1):
            port_b = free_port_pair()
        path_a, app_a, nonce_a = project("app-a", port_a)
        path_b, app_b, nonce_b = project("app-b", port_b)
        url_a = deploy("deploy-a", names[0], path_a, app_a)
        url_b = deploy("deploy-b", names[1], path_b, app_b)
        assert url_a != url_b
        verify("http-a", url_a, nonce_a)
        verify("http-b", url_b, nonce_b)
        registry_path, registry_app, registry_nonce = project("registry-app", free_port_pair())
        registry_url = deploy("deploy-registry", names[1], registry_path, registry_app, "--chunking=off")
        verify("http-registry", registry_url, registry_nonce)
        assert deploy("unchanged-a", names[0], path_a, app_a) == url_a
        verify("http-unchanged-a", url_a, nonce_a)
        cli("stop-app-a", "--device", f"vm:{names[0]}", "device", "apps", "stop", app_a)
        assert deploy("restart-a", names[0], path_a, app_a) == url_a
        verify("http-restart-a", url_a, nonce_a)
        text = cli("text-a", "--json=false", "--device", f"vm:{names[0]}", "run", "--detach", "--yes",
                   "--skip-cloud-registration", "--prefix", str(path_a))
        assert not text.stdout and url_a in text.stderr and "Readiness not checked" in text.stderr

        # A different VM cannot steal A's host listener or report its endpoint.
        conflict_path, _, _ = project("same-port", port_a)
        conflict = cli("cross-vm-conflict", "--json", "--device", f"vm:{names[1]}", "run", "--detach", "--yes",
                       "--skip-cloud-registration", "--prefix", str(conflict_path), expected=1)
        assert not conflict.stdout and "forward" in conflict.stderr.lower()
        verify("http-a-after-conflict", url_a, nonce_a)

        # A non-Wendy listener stays owned by its creator when forwarding fails.
        with socket.socket() as blocker:
            blocker.bind(("127.0.0.1", 0))
            blocker.listen()
            blocked_port = blocker.getsockname()[1]
            blocked_path, _, _ = project("blocked-port", blocked_port)
            conflict = cli("host-port-conflict", "--json", "--device", f"vm:{names[1]}", "run", "--detach", "--yes",
                           "--skip-cloud-registration", "--prefix", str(blocked_path), expected=1)
            assert not conflict.stdout and "forward" in conflict.stderr.lower()
            with socket.create_connection(("127.0.0.1", blocked_port), timeout=3):
                blocker.settimeout(3)
                connection, _ = blocker.accept()
                connection.close()
        steps.append({"step": "complete", "passed": True})
        print(f"Passed detached endpoint VM journey. Evidence: {root}")
    finally:
        cleanup_errors = []
        for name in names:
            if (config / "vms" / name).exists():
                for action, extra in (("stop", ["--force"]), ("rm", [])):
                    try:
                        cli(f"cleanup-{action}-{name}", "vm", action, name, *extra, "--yes", timeout=60)
                    except Exception as error:
                        cleanup_errors.append(str(error))
        (root / "results.json").write_text(json.dumps(steps, indent=2) + "\n")
        if cleanup_errors:
            raise RuntimeError("VM cleanup failed: " + "; ".join(cleanup_errors))


if __name__ == "__main__":
    main()
