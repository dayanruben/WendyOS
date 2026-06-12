import Testing

/// Hidden deprecated compatibility command for `wendy run` with cloud routing.
///
/// Use `wendy run --device <name>` in new scripts and documentation.
@Suite
struct `'wendy cloud run'` {
    // MARK: - Compatibility

    /**
     The hidden command remains directly invocable for older scripts, but
     parent help and completions do not advertise it. Direct help identifies
     `wendy run` as the replacement command.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `is hidden from parent help while direct help points to 'wendy run'`() async throws {
        // TODO: implement.
    }

    /**
     In human-readable mode, the command delegates to `wendy run` with cloud
     device context and writes a deprecation warning that points to `wendy run`.
     The warning is emitted before build, auth, or tunnel failures so users see
     the replacement even when the legacy invocation cannot complete.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `delegates to 'wendy run' with a deprecation notice`() async throws {
        // TODO: implement.
    }

    /**
     With `--json` or non-interactive JSON output, deprecation guidance stays
     out of stdout and stderr so existing automation can continue parsing the
     response.
     */
    @Test(.disabled("SPEC STUB: behavior agreed, implementation pending"))
    func `'--json' keeps JSON output clean`() async throws {
        // TODO: implement.
    }
}
