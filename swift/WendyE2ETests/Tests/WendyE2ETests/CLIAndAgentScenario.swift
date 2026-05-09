import Foundation
import WendyE2ETesting

final class CLIAndAgentScenario: Scenario, Sendable {
    static var shared: CLIAndAgentScenario {
        get async {
            _shared
        }
    }

    let cli: Machine
    let agent: Machine

    private static let _shared = CLIAndAgentScenario()

    private init() {
        let repositoryRootDirectoryURL = Self.repositoryRootDirectoryURL()

        self.cli = Machine(
            id: "cli",
            name: "CLI",
            os: Environment.cliOS ?? .current,
            tags: [.cli],
            ssh: Environment.cliSSH,
            workingDirectory: Environment.cliWorkingDirectory
                ?? repositoryRootDirectoryURL.appendingPathComponent("go").path
        )

        self.agent = Machine(
            id: "agent",
            name: "Agent",
            os: Environment.agentOS ?? .current,
            tags: [.agent],
            ssh: Environment.agentSSH,
            workingDirectory: Environment.agentWorkingDirectory
                ?? repositoryRootDirectoryURL.appendingPathComponent("swift").path
        )
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
