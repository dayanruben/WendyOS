# WendyAgentE2ETests

Minimal Swift E2E scaffolding built around a local-or-SSH `Machine` helper.

## Run tests

```bash
cd swift/WendyAgentE2ETests
swift test
```

## Machine configuration

`Machine` takes an optional SSH target and optional working directory:

```swift
let remote = Machine(ssh: "user@host", path: "/path/to/repo")
let local = Machine(path: "/path/to/repo")
```

If `ssh` is omitted, commands run on the local machine with `path` as their
working directory, defaulting to the current directory. If remote `path` is
omitted, commands run in the SSH user's home directory. Each remote command runs
in its own SSH invocation.

## Run the smoke test

The smoke test is gated behind `WENDY_E2E_SMOKE=1`. Set `E2E_MACHINE_SSH` for a
remote machine, or omit it to run locally. `E2E_MACHINE_PATH` is optional:

```bash
cd swift/WendyAgentE2ETests
WENDY_E2E_SMOKE=1 \
E2E_MACHINE_SSH='user@host' \
E2E_MACHINE_PATH='/path/to/wendy-agent' \
swift test --filter MachineSmokeTests
```
