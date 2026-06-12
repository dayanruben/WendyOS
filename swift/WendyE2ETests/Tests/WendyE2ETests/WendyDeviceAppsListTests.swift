import Testing

@Suite
struct `'wendy device apps list'` {
    /**
     Displays usage for `wendy device apps list`. The output includes the
     command synopsis, local flags, inherited global flags, and concise
     descriptions. Help exits successfully, writes to stdout, emits no
     stderr, and leaves configuration, cache, project, cloud, and device
     state untouched.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `prints command help`() async throws {
        // TODO: implement.
    }

    /**
     `--device` selects the target device hostname and skips discovery and
     pickers. The command does not read or change the saved default device when
     an explicit target is supplied.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `uses explicit device selection without prompting`() async throws {
        // TODO: implement.
    }

    /**
     Without an explicit or configured device in a non-interactive context,
     reports that a device selection is required, emits no prompt escape
     sequences, and performs no device operation.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `reports missing device selection in non-interactive mode`() async throws {
        // TODO: implement.
    }

    /**
     Connection failures, timeouts, and incompatible agent responses produce
     stderr diagnostics and a failure status. Output does not claim that the
     operation succeeded.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `reports unreachable devices without partial success`() async throws {
        // TODO: implement.
    }

    /**
     Displays deployed applications with names, images, status, restart policy,
     and relevant ports. An empty device reports an empty successful list.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `lists deployed applications`() async throws {
        // TODO: implement.
    }

    /**
     With `--json`, emits application objects with stable field names and value
     types. JSON mode emits no table formatting and no stderr on success.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `prints JSON application inventory`() async throws {
        // TODO: implement.
    }

    /**
     Accepts only the documented arguments and flags for `wendy device apps
     list`. Extra positional arguments or unknown flags produce a usage
     diagnostic on stderr, return a failure status, emit no success output,
     and leave existing state unchanged.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `rejects undocumented arguments and flags`() async throws {
        // TODO: implement.
    }
}

/// Public compatibility alias for `wendy device apps list`.
///
/// The short `ps` form remains visible in help for users who expect a
/// container-style listing command.
@Suite
struct `'wendy device ps'` {
    // MARK: - Compatibility

    /**
     Displays usage for `wendy device ps`. The output identifies the command as
     an alias for `wendy device apps list`, lists the same inherited global
     flags, exits successfully, and emits no stderr.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `prints '... device ps' alias help`() async throws {
        // TODO: implement.
    }

    /**
     Produces the same human-readable application inventory as `wendy device
     apps list`, including empty-device output and table formatting. The alias
     does not introduce additional prompts or state changes.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `aliases '... device apps list'`() async throws {
        // TODO: implement.
    }

    /**
     With `--json`, emits the same application inventory schema as `wendy device
     apps list` and keeps stdout machine-readable for automation.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `keeps '... device apps list --json' output clean`() async throws {
        // TODO: implement.
    }
}
