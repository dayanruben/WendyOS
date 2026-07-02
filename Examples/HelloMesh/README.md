# HelloMesh

Reach a service running on **another WendyOS device** over the mesh.

## What it demonstrates

The whole point is one entitlement on the `client` service in
[`wendy.json`](./wendy.json):

```json
{ "type": "network", "mode": "mesh", "serviceCIDR": "10.99.0.0/16" }
```

`mode: "mesh"` grants the container **egress to the mesh service CIDR**. On
container start, WendyOS:

1. adds a route inside the container's network namespace: `10.99.0.0/16` → the
   bridge gateway, and
2. adds an ACCEPT rule scoped to *this container's* IP in the host's
   `WENDY-MESH` firewall chain.

The container can then talk to any address in that CIDR as if it were local —
`client/app.py` just polls one such address and logs the result.

## Why `isolation: "isolated"`

Mesh egress needs the container to have **its own network namespace and
bridge** — that only happens on WendyOS's *isolated* networking path. A plain
host-networked app shares the host stack and has no per-container netns for a
mesh route to live in. So this example is structured as an
`isolation: "isolated"` app with a named `client` service; that's what makes the
agent run the CNI bridge setup and apply the mesh route/rule. (A single service
is enough — it does not need to be multi-service.)

## Topology

```
┌─ device A ─────────────┐         mesh          ┌─ device B ─────────────┐
│  HelloMesh / client     │  ── 10.99.0.0/16 ──▶  │  a service published    │
│  network: mode=mesh     │                       │  into the mesh at       │
│  serviceCIDR=10.99/16   │                       │  10.99.0.10:8080        │
└────────────────────────┘                        └─────────────────────────┘
```

## Run it

```bash
cd examples/HelloMesh
wendy run
```

Environment variables `client/app.py` reads:

```bash
MESH_TARGET=device-1.cloud.wendy.dev:8080   # mesh hostname:port (default: device-1)
POLL_INTERVAL=5                              # seconds between polls (default)
```

Logs look like:

```
[hello-mesh] starting; polling http://device-1.cloud.wendy.dev:8080/ every 5s over the mesh
[hello-mesh] OK 200 from device-1.cloud.wendy.dev:8080: 'hello from device B'
```

## How to use it

The mesh data plane is now implemented. Polling `device-<assetID>.cloud.wendy.dev:8080` reaches host port 8080 on that device — LAN-direct when both devices are on the same network, or relayed via the cloud broker when needed.

**Find your device's asset ID** (a number, 1–65534):

```bash
# Option 1: List devices in your org. The default table doesn't show the
# asset ID — use --json (or the copy-JSON key) to read it.
wendy cloud discover --json

# Option 2: Check the cloud dashboard (dashboard.wendy.dev)
```

Once you have the asset ID (e.g., `215`), set:

```bash
MESH_TARGET=device-215.cloud.wendy.dev:8080 wendy run
```

On success, logs will show:

```
[hello-mesh] OK 200 from device-215.cloud.wendy.dev:8080: 'hello from device B'
```

**Control plane notes:**

- **Organization-level**: Mesh is **enabled by default**. It will be controllable org-wide (admins can disable it via the cloud API) once the cloud-side update ships.
- **Device-level**: A device-local `mesh-disabled` file in the agent config directory (`/etc/wendy-agent/`) disables mesh serving on that device.
- **Cloud relay**: LAN-direct peering works on this branch, but **both** devices must run this branch's agent — the serving side's `MeshDial` RPC is new, so dialing an older-agent peer fails. Cloud relay fallback requires a cloud-side update (not yet shipped).
- **After an agent restart/reboot**, re-run `wendy run` (recreate) to restore mesh networking — the reconcile path recreates the container's resolv.conf so it starts, but does not yet rewire CNI networking/mesh egress.

## Debugging the plumbing

To verify the mesh entitlement, route, and firewall rules are in place:

```bash
# ACCEPT rule for the container's IP → mesh serviceCIDR
iptables -S WENDY-MESH

# REDIRECT rule (NAT table) that intercepts mesh traffic
iptables -t nat -S WENDY-MESH

# Inside the container's network namespace:
# (substitute the container PID from `nerdctl inspect <container>` or similar)
nsenter -t <container-pid> -n ip route    # 10.99.0.0/16 via <bridge gateway>
```
