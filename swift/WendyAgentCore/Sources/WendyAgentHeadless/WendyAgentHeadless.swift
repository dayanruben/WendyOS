import Foundation
import WendyAgentCore

#if canImport(Darwin)
    import Darwin
#elseif canImport(Glibc)
    import Glibc
#endif

/// Headless Swift Mac agent entrypoint used by managed Swift E2E runs.
@main
struct WendyAgentHeadless {
    @MainActor
    static func main() async {
        if CommandLine.arguments.dropFirst().contains("--version") {
            print(WendyAgent.version)
            return
        }

        Self.blockTerminationSignals()

        let agent = WendyAgent(configuration: .environment)
        do {
            try await agent.start()
            _ = await Self.waitForTerminationSignal()
            await agent.stop()
        } catch {
            FileHandle.standardError.write(
                Data("wendy-agent-swift failed: \(error)\n".utf8)
            )
            Foundation.exit(EXIT_FAILURE)
        }
    }

    private static func blockTerminationSignals() {
        var signalSet = Self.terminationSignalSet()
        pthread_sigmask(SIG_BLOCK, &signalSet, nil)
    }

    private static func waitForTerminationSignal() async -> Int32 {
        await Task.detached {
            var signalSet = Self.terminationSignalSet()
            var receivedSignal: Int32 = 0
            sigwait(&signalSet, &receivedSignal)
            return receivedSignal
        }.value
    }

    private static func terminationSignalSet() -> sigset_t {
        var signalSet = sigset_t()
        sigemptyset(&signalSet)
        sigaddset(&signalSet, SIGINT)
        sigaddset(&signalSet, SIGTERM)
        return signalSet
    }
}

extension WendyAgentConfiguration {
    fileprivate static var environment: Self {
        let environment = ProcessInfo.processInfo.environment
        return Self(
            port: Self.intValue(
                named: "WENDY_AGENT_PORT",
                in: environment,
                default: 50051
            ),
            otelPort: Self.intValue(
                named: "WENDY_OTEL_PORT",
                in: environment,
                default: 4317
            ),
            appPath: environment["WENDY_AGENT_APP_PATH"] ?? "",
            sandboxProfile: environment["WENDY_AGENT_SANDBOX_PROFILE"] ?? ""
        )
    }

    private static func intValue(
        named name: String,
        in environment: [String: String],
        default defaultValue: Int
    ) -> Int {
        guard let value = environment[name], let intValue = Int(value) else {
            return defaultValue
        }
        return intValue
    }
}
