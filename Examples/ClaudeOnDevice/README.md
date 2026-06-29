# Claude-on-device

Runs the [Claude Code](https://claude.com/claude-code) CLI inside an
`admin`-entitled container on a WendyOS device, so the device can **operate and
debug itself** over the local agent socket. You log in with a normal Claude.ai
subscription; Claude then drives the device through the in-container `wendy` CLI
(pre-pointed at the agent's local socket) — reading device info, listing and
controlling apps, streaming logs/telemetry, and execing into other containers.

## ⚠️ Security: `admin` is a full-control grant

The `admin` entitlement bind-mounts the agent's control socket into this
container with **no authentication** — the entitlement mount is the entire trust
boundary. Anything running here can start/stop/**delete** any app, read all
telemetry, exec into any container, and trigger **OS/agent updates** — i.e. it
can brick or wipe the device if adversarially prompted. Your Claude.ai OAuth
token is also stored on the device (`/root/.claude` volume). **Deploy only to
trusted, first-party devices.**

## Build & deploy (from an amd64 dev host)

1. **Deploy a new-enough agent first.** The `ExecContainer` PTY RPC and the
   `admin` socket only exist in an agent built from the claude-on-device branch.
   Update the device's agent before deploying this app:
   ```
   wendy device update --binary <path-to-arm64-wendy-agent> --device <host>
   ```
2. **Stage the arm64 `wendy` CLI** into this directory as `wendy-linux-arm64`
   (built from the same branch, so it understands `WENDY_AGENT_SOCKET`):
   ```
   GOOS=linux GOARCH=arm64 go build -o Examples/ClaudeOnDevice/wendy-linux-arm64 ./go/cmd/wendy
   ```
3. **Build + deploy** the app to the device (Dockerfile-driven, cross-built for arm64):
   ```
   cd Examples/ClaudeOnDevice
   wendy run --yes --build-type docker --device <jetson-hostname>
   ```

## Log in & use

Attach an interactive terminal and run Claude:

```
wendy device attach claude-on-device
```

On first run, `claude` prints an OAuth URL + code — approve it in your laptop
browser and paste the code back into the attached session. The session token
persists to the `/root/.claude` volume and survives container restarts.

Then just talk to Claude. It can run, on the device, over the local socket:

```
wendy device info
wendy device apps
wendy device telemetry logs <app>
wendy device attach <other-app> -- /bin/sh
```

`wendy-linux-arm64` is intentionally git-ignored — it is a build artifact you
stage locally, not source.
