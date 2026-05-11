import Foundation
import WendyE2ETesting

final class CLIAndAgentScenario: Scenario, Sendable {
    static var shared: CLIAndAgentScenario {
        get async throws {
            try await _shared.value
        }
    }

    let cli: Session
    let agent: Session

    private static let _shared = Task {
        try await CLIAndAgentScenario()
    }

    private init() async throws {
        let repositoryRootDirectoryURL = Self.repositoryRootDirectoryURL()

        let cli = Machine(
            id: "cli",
            name: "CLI",
            os: Environment.cliOS ?? .current,
            tags: [.cli],
            ssh: Environment.cliSSH,
            workingDirectory: Environment.cliWorkingDirectory
                ?? repositoryRootDirectoryURL.appendingPathComponent("go").path
        )

        let agent = Machine(
            id: "agent",
            name: "Agent",
            os: Environment.agentOS ?? .current,
            tags: [.agent],
            ssh: Environment.agentSSH,
            workingDirectory: Environment.agentWorkingDirectory
                ?? repositoryRootDirectoryURL.appendingPathComponent("swift").path
        )

        self.cli = try await Session.begin(for: cli)
        self.agent = try await Session.begin(for: agent)

        try await self.buildCLI(with: self.cli)
        try await self.buildAgent(with: self.agent)
    }

    deinit {
        let cli = self.cli
        let agent = self.agent

        // WORKAROUND: Swift Testing does not provide an async tear-down hook.
        // Suite life-cycle is init/deinit based and Swift has no async deinit,
        // so session clean-up has to be bridged through an unstructured task.
        // Fix by finding a structured concurrency solution for this.
        Task {
            try? await agent.end()
            try? await cli.end()
        }
    }

    private func buildCLI(with session: Session) async throws {
        switch session.machine.os {
        case .macOS, .linux:
            try await session.sh("make build-cli")
        case .windows, .wendyOS:
            break
        }
    }

    private func buildAgent(with session: Session) async throws {
        switch session.machine.os {
        case .macOS:
            try await session.sh("make build-dev")
        case .linux:
            try await session.sh("cd ../go && make build-agent")
        case .windows, .wendyOS:
            break
        }
    }

    private static func repositoryRootDirectoryURL() -> URL {
        URL(fileURLWithPath: #filePath, isDirectory: false)
            .deletingLastPathComponent()  // Tests/WendyE2ETests
            .deletingLastPathComponent()  // Tests
            .deletingLastPathComponent()  // swift/WendyE2ETests
            .deletingLastPathComponent()  // swift
            .deletingLastPathComponent()  // repository root
    }
}
