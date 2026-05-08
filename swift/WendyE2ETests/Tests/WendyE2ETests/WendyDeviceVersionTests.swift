import Testing

@Suite(.serialized)
struct `'wendy device version'` {
    /**
     Prints the version summary for a device selected explicitly by the user.

     This command is the quick health check for a reachable Wendy agent. The output identifies the agent, the operating system it runs on, the CPU architecture, and the local CLI version involved in the interaction.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `prints agent details from an explicit local device`() async throws {
        // Given: the local macOS app-backed Wendy agent is running and listening on the agent gRPC port
        // When: `wendy device version --device 127.0.0.1` is run
        // Then:
        // - exits successfully
        // - prints the agent version
        // - prints the agent OS and OS version
        // - prints the agent CPU architecture
        // - prints the CLI version
        // - emits only expected connection/provisioning guidance on stderr
        // - does not prompt for device selection
    }

    /**
     Renders the version summary as machine-readable JSON.

     JSON mode is for tools and automation. The output contains the same device and CLI facts as the human-readable summary without terminal styling, progress text, or interactive prompts.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `'--json' prints agent details as JSON`() async throws {
        // Given: the local macOS app-backed Wendy agent is running and listening on the agent gRPC port
        // When: `wendy --json device version --device 127.0.0.1` is run
        // Then:
        // - exits successfully
        // - emits a single valid JSON object on stdout
        // - includes version, os, osVersion, cpuArchitecture, cliVersion, and hasGpu fields
        // - uses strings for version, os, osVersion, cpuArchitecture, and cliVersion
        // - uses a boolean for hasGpu
        // - emits no stderr diagnostics or provisioning hints
        // - does not prompt for device selection or update confirmation
    }

    /**
     Keeps the informational alias equivalent to the primary command.

     Users who discover or remember `device info` get the same read-only device summary as `device version`.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `'device info' prints the same agent details`() async throws {
        // Given: the local macOS app-backed Wendy agent is running and listening on the agent gRPC port
        // When: `wendy device info --device 127.0.0.1` is run
        // Then:
        // - exits successfully
        // - prints the same semantic fields as `wendy device version --device 127.0.0.1`
        // - emits only expected connection/provisioning guidance on stderr
        // - does not prompt for device selection
    }

    /**
     Fails clearly when automation has not selected a device.

     JSON usage is prompt-free. A missing explicit device and missing default device appear as a configuration problem that automation can detect from the exit status and diagnostic.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `'--json' reports a missing device without prompting`() async throws {
        // Given: an isolated HOME/config directory with no default device configured
        // When: `wendy --json device version` is run without `--device`
        // Then:
        // - exits unsuccessfully
        // - emits no JSON payload on stdout
        // - prints a clear stderr diagnostic explaining how to select or configure a device
        // - does not open an interactive device picker
        // - does not mutate user configuration
    }

    /**
     Reports connection failures for an explicitly selected unreachable device.

     An explicit `--device` value is the user's target selection. If the agent cannot be reached, the command reports that target as unreachable instead of falling back to discovery or masking the connection failure.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `reports an unreachable explicit device`() async throws {
        // Given: no Wendy agent is listening at an unused loopback address or port
        // When: `wendy device version --device <unreachable-loopback-endpoint>` is run
        // Then:
        // - exits unsuccessfully
        // - emits no successful version summary on stdout
        // - prints a clear stderr diagnostic for the failed connection
        // - does not open an interactive device picker
        // - does not mutate user configuration
    }

    /**
     Uses a configured default device when no explicit device is passed.

     A default device is the user's saved target selection. The command reports the same version summary for that saved target as it does for an explicit `--device` invocation.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `uses the configured default device`() async throws {
        // Given: an isolated HOME/config directory where the default device is `127.0.0.1`
        // And: the local macOS app-backed Wendy agent is running and listening on the agent gRPC port
        // When: `wendy device version` is run without `--device`
        // Then:
        // - exits successfully
        // - prints the agent version, OS, architecture, and CLI version
        // - emits only expected connection/provisioning guidance on stderr
        // - does not prompt for device selection
        // - does not rewrite the default-device configuration
    }

    /**
     Surfaces version skew when the agent is older than the CLI.

     Version output includes compatibility guidance. When the agent reports an older version, the summary remains available and includes a human-readable update hint.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `warns when the agent is behind the CLI`() async throws {
        // Given: a deterministic agent fixture that reports an agent version older than the CLI version
        // When: `wendy device version --device <fixture-device>` is run
        // Then:
        // - exits successfully
        // - prints the agent and CLI versions
        // - includes a human-readable warning that the agent is behind the CLI
        // - tells the user how to update the agent
        // - does not attempt an update unless the user explicitly requested one
    }

    /**
     Surfaces version skew when the CLI is older than the agent.

     Users can diagnose compatibility issues from the version command alone. When the agent reports a newer version, the summary remains available and explains that the local CLI may need an update.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `warns when the CLI is behind the agent`() async throws {
        // Given: a deterministic agent fixture that reports an agent version newer than the CLI version
        // When: `wendy device version --device <fixture-device>` is run
        // Then:
        // - exits successfully
        // - prints the agent and CLI versions
        // - includes a human-readable warning that the CLI is behind the agent
        // - does not attempt to modify the CLI or agent
    }

    /**
     Reports optional hardware fields when the agent provides them.

     Device type, storage, and GPU metadata enrich the version summary. Human output includes the fields that are present and leaves absent optional fields out of the summary.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `prints optional hardware details when reported by the agent`() async throws {
        // Given: a deterministic agent fixture that reports device type, storage medium, GPU vendor, JetPack, and CUDA details
        // When: `wendy device version --device <fixture-device>` is run
        // Then:
        // - exits successfully
        // - prints the required version, OS, architecture, and CLI fields
        // - prints device type and storage fields
        // - prints GPU, JetPack, and CUDA fields
        // - omits no reported optional fields
    }
}
