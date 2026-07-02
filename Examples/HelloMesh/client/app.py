#!/usr/bin/env python3
"""HelloMesh client — reach a service on another WendyOS device over the mesh.

This service declares a `network` entitlement with `mode: "mesh"` (see the
parent wendy.json). Because the app runs in `isolation: "isolated"` mode, the
service gets its own network namespace and bridge, and the mesh entitlement
grants it egress to the mesh service CIDR: WendyOS adds a route (serviceCIDR ->
bridge gateway) inside the container's netns and an ACCEPT rule scoped to this
container's IP in the host's WENDY-MESH firewall chain. The container can then
address services published by peers in that CIDR as if they were local.

The client polls a target address inside the mesh CIDR and logs the result.
"""

import os
import sys
import time
import urllib.error
import urllib.request

# A host:port inside the mesh serviceCIDR (see wendy.json). Point this at a
# service another device publishes into the mesh.
TARGET = os.environ.get("MESH_TARGET", "device-1.cloud.wendy.dev:8080")
INTERVAL = float(os.environ.get("POLL_INTERVAL", "5"))
URL = f"http://{TARGET}/"


def log(msg: str) -> None:
    print(f"[hello-mesh] {msg}", file=sys.stdout, flush=True)


def main() -> None:
    log(f"starting; polling {URL} every {INTERVAL:g}s over the mesh")
    while True:
        try:
            with urllib.request.urlopen(URL, timeout=3) as resp:
                body = resp.read(200).decode("utf-8", "replace").strip()
                log(f"OK {resp.status} from {TARGET}: {body!r}")
        except urllib.error.URLError as exc:
            # With the mesh route present, a timeout / connection-refused here
            # means the route + firewall are in place but nothing is serving
            # that address yet. Once a peer publishes a service into the mesh
            # CIDR, this turns into OK responses.
            log(f"no response from {TARGET}: {exc.reason}")
        except Exception as exc:  # noqa: BLE001 - example: log anything and keep going
            log(f"error contacting {TARGET}: {exc}")
        time.sleep(INTERVAL)


if __name__ == "__main__":
    main()
