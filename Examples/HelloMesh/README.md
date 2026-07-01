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
MESH_TARGET=10.99.0.10:8080   # host:port inside serviceCIDR (default)
POLL_INTERVAL=5               # seconds between polls (default)
```

Logs look like:

```
[hello-mesh] starting; polling http://10.99.0.10:8080/ every 5s over the mesh
[hello-mesh] OK 200 from 10.99.0.10:8080: 'hello from device B'
```

## Current status (read this)

The **foundation** wires the entitlement to the route + firewall rule above —
that is what this example exercises. The **data plane** that actually
*publishes* a peer's service into the mesh CIDR (service-proxy / VIP listeners,
peer mTLS over the Cloud PKI, LAN-first-then-broker rendezvous) is a separate,
in-progress piece.

Until that lands, `MESH_TARGET` won't answer and you'll see:

```
[hello-mesh] no response from 10.99.0.10:8080: <timeout/connection refused>
```

That log is the useful signal at this stage: it confirms the container **has**
the mesh entitlement, route, and firewall ACCEPT rule in place (traffic leaves
the container toward the gateway) — there's simply nothing serving that address
yet. To verify the plumbing directly, on the device host:

```bash
iptables -S WENDY-MESH                    # ACCEPT rule for the container's /32 → 10.99.0.0/16
nsenter -t <container-pid> -n ip route    # 10.99.0.0/16 via <bridge gateway>
```
