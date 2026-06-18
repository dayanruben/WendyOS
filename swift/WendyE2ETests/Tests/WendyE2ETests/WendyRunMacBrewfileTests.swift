import Testing

@Suite
struct `'wendy run' native Mac Brewfile support` {
    /**
     Copies the native Mac app's Brewfile to the selected Wendy Agent for Mac
     target and runs `brew bundle --file <synced Brewfile>` on that target
     before starting the app.

     The fixture is a SwiftPM project with `platform: "darwin"` and a
     project-root `Brewfile.wendy`. The command builds locally, syncs the app
     and `Brewfile.wendy`, applies the synced Brewfile on the agent machine,
     and only then starts the app. The app prints evidence that the dependency
     is available at runtime. The developer machine must not run `brew bundle`
     as part of this flow.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `syncs the Brewfile and runs brew bundle on the target before starting the app`() async throws {
        // TODO: implement.
    }

    /**
     A native Xcode project targeting `platform: "darwin"` behaves the same as
     the SwiftPM path: `Brewfile.wendy` is auto-detected, synced through the
     Mac native file-sync path, applied on the target Mac, and only then is the
     built product started.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `auto-detects Brewfile dot wendy for Xcode Mac apps and applies it on the target`() async throws {
        // TODO: implement.
    }

    /**
     A plain project-root `Brewfile` is treated as developer-machine setup and
     is not auto-applied to the target Mac. With no `brewfile` field and no
     `Brewfile.wendy`, `wendy run` starts the native app without invoking
     target-side Homebrew.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `ignores project root Brewfile unless explicitly configured`() async throws {
        // TODO: implement.
    }

    /**
     When `wendy.json` sets an explicit relative `brewfile` path, such as
     `ops/Brewfile`, Wendy syncs that exact relative path and applies that file
     on the target Mac. The explicit path takes precedence over any
     `Brewfile.wendy` in the project root.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `uses explicit relative brewfile path instead of auto-detected Brewfile dot wendy`() async throws {
        // TODO: implement.
    }

    /**
     If Homebrew is missing from the target Mac, `wendy run` fails before
     starting the app and reports an actionable error that identifies the target
     prerequisite. Wendy must not install Homebrew implicitly, and JSON output
     should contain a stable failure shape without interactive prompts.
     */
    @Test(.disabled("SPEC STUB: requires target without Homebrew or controllable brew path"))
    func `reports missing Homebrew on the target Mac without starting the app`() async throws {
        // TODO: implement.
    }

    /**
     If target-side `brew bundle --file <synced Brewfile>` exits non-zero,
     `wendy run` fails before starting the app and includes the Brewfile path,
     exit status, and useful bundle output in the diagnostic. Plain output and
     `--json` output should both be deliberate and non-interactive.
     */
    @Test(.disabled("SPEC STUB: requires controllable failing target-side brew bundle"))
    func `surfaces target brew bundle failures with context and does not start the app`() async throws {
        // TODO: implement.
    }

    /**
     Running the same native Mac app twice with the same Brewfile is idempotent:
     already-satisfied dependencies do not cause a failure, noisy reinstall
     loops, or repeated file-transfer churn beyond normal manifest checks.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `re-running with the same Brewfile is idempotent`() async throws {
        // TODO: implement.
    }

    /**
     If `wendy.json` declares `brewfile: "ops/Brewfile"` while `files` maps a
     different local file to the same target path, the CLI rejects the project
     before deployment. No app files are synced, no target-side `brew bundle`
     is invoked, and the error explains how to remove the duplicate mapping or
     point `brewfile` at the same source.
     */
    @Test(.disabled("SPEC STUB: can run with fake or real Mac target once E2E harness exists"))
    func `rejects files mappings that conflict with the configured Brewfile destination`() async throws {
        // TODO: implement.
    }
}
