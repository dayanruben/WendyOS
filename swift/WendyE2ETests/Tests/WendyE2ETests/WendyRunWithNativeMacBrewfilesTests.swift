import Testing

@Suite
struct `'wendy run' with native Mac Brewfiles` {
    /**
     Copies the native Mac app's `Brewfile.wendy` to the selected Wendy Agent
     for Mac target and runs `brew bundle` on that target before starting the
     app.

     The fixture is a SwiftPM project with `platform: "darwin"`, a project-root
     `Brewfile.wendy`, and an app that can prove the bundled dependency is
     available at runtime. The command runs as `wendy run --json --device
     <mac-agent> --prefix <project>`. It builds locally, syncs the app and
     `Brewfile.wendy`, invokes `brew bundle --file <synced Brewfile>` on the
     agent machine, then starts the app. The command recording and target-side
     evidence show that `brew bundle` did not run on the developer machine.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `syncs the 'Brewfile.wendy' and runs 'brew bundle' on the target before starting`() async throws {
        // TODO: implement.
    }

    /**
     Applies the same target-side `Brewfile.wendy` and `brew bundle` behavior
     for native Xcode projects.

     The fixture is an Xcode project with `platform: "darwin"` and a
     project-root `Brewfile.wendy`. `wendy run --json --device <mac-agent>
     --prefix <project>` builds with `xcodebuild`, syncs the build product and
     `Brewfile.wendy`, runs `brew bundle --file <synced Brewfile>` on the Mac
     agent, and starts the built product only after `brew bundle` succeeds.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `syncs 'Brewfile.wendy' and runs 'brew bundle' for Xcode apps`() async throws {
        // TODO: implement.
    }

    /**
     Leaves a project-root `Brewfile` for developer-machine setup unless the
     project explicitly opts into using it for the target.

     The fixture contains `Brewfile` but no `Brewfile.wendy` and no `brewfile`
     field in `wendy.json`. `wendy run --json --device <mac-agent>` starts the
     native Mac app without syncing that `Brewfile` as a target dependency file
     and without invoking target-side `brew bundle`.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `does not auto-apply a plain project root 'Brewfile'`() async throws {
        // TODO: implement.
    }

    /**
     Uses an explicit `wendy.json` `brewfile` path as the target Brewfile,
     overriding project-root auto-detection.

     The fixture contains both `Brewfile.wendy` and `ops/Brewfile`, while
     `wendy.json` sets `"brewfile": "ops/Brewfile"`. `wendy run --json
     --device <mac-agent>` syncs and applies `ops/Brewfile`, does not apply
     `Brewfile.wendy`, and starts the app only after the explicit Brewfile has
     succeeded on the target Mac.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `uses the explicit 'brewfile' path instead of 'Brewfile.wendy' auto detection`() async throws {
        // TODO: implement.
    }

    /**
     Fails before app start when `brew` is missing on the target Mac.

     The fixture has a valid native Mac app and target Brewfile, but the Mac
     agent cannot resolve `brew` from `PATH`, `/opt/homebrew/bin/brew`, or
     `/usr/local/bin/brew`. `wendy run --json --device <mac-agent>` returns a
     failure status, reports that Homebrew must be installed on the target Mac,
     emits no interactive prompts, and leaves the app stopped.
     */
    @Test(.disabled("SPEC STUB: requires target without Homebrew or controllable brew path"))
    func `reports missing 'brew' on the target without starting the app`() async throws {
        // TODO: implement.
    }

    /**
     Fails before app start when target-side `brew bundle` fails.

     The fixture uses a Brewfile that makes `brew bundle --file <synced
     Brewfile>` exit non-zero on the Mac agent. `wendy run --json --device
     <mac-agent>` returns a failure status, includes the Brewfile path, exit
     status, and useful bundle output in the diagnostic, emits no success
     message, and leaves the app stopped.
     */
    @Test(.disabled("SPEC STUB: requires controllable failing target-side brew bundle"))
    func `reports 'brew bundle' failures without starting the app`() async throws {
        // TODO: implement.
    }

    /**
     Remains idempotent when the target already satisfies the Brewfile.

     The fixture runs the same native Mac app with the same target Brewfile
     twice against the same Mac agent. The second `wendy run --json --device
     <mac-agent>` succeeds, does not reinstall already-satisfied dependencies in
     a noisy loop, reports normal file-sync up-to-date behavior where possible,
     and starts the app after the target-side bundle check succeeds.
     */
    @Test(.disabled("SPEC STUB: requires Mac agent E2E fixture"))
    func `is idempotent when 'brew bundle' dependencies are already installed`() async throws {
        // TODO: implement.
    }

    /**
     Rejects ambiguous sync configuration before deployment starts.

     The fixture sets `"brewfile": "ops/Brewfile"` while `files` maps a
     different local file to the same target path, for example `"path":
     "dev/Brewfile", "to": "ops/Brewfile"`. `wendy run --json --device
     <mac-agent>` fails during local project validation, explains that the
     Brewfile destination conflicts with another synced file, syncs no app
     files, invokes no target-side `brew bundle`, and leaves any existing app
     state unchanged.
     */
    @Test(.disabled("SPEC STUB: can run with fake or real Mac target once E2E harness exists"))
    func `rejects 'files' entries that conflict with the 'brewfile' destination`() async throws {
        // TODO: implement.
    }
}
