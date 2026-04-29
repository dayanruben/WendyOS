import Foundation
import Subprocess
import Testing
import WendyE2ETesting

struct AgentE2ETests {
    @Test("build CLI and agent", .timeLimit(.minutes(10)))
    func buildCLIAndAgent() async throws {
        let rootDirectoryURL = Self.rootDirectoryURL()

        let goDirectoryPath = rootDirectoryURL.appendingPathComponent("go").path
        let swiftDirectoryPath = rootDirectoryURL.appendingPathComponent("swift").path

        let cli = Machine(name: "CLI", workingDirectory: goDirectoryPath)
        let agent = Machine(name: "Agent", workingDirectory: swiftDirectoryPath)

        let cliBuild = try await cli.run(
            "make build",
            output: .string(limit: .max),
            error: .string(limit: .max)
        )
        let cliBuildOutput = (cliBuild.standardOutput ?? "") + (cliBuild.standardError ?? "")

        #expect(cliBuild.terminationStatus.isSuccess)
        #expect(Self.output(cliBuildOutput, contains: #"go build .* bin/wendy"#))
        #expect(Self.output(cliBuildOutput, contains: #"go build .* bin/wendy-agent"#))

        try await agent.run("make build-dev")

        print("All done!")
    }

    private static func output(_ output: String, contains pattern: String) -> Bool {
        output.range(of: pattern, options: String.CompareOptions.regularExpression) != nil
    }

    private static func rootDirectoryURL() -> URL {
        URL(fileURLWithPath: #filePath, isDirectory: false)
            .deletingLastPathComponent()  // Tests/WendyAgentE2ETests
            .deletingLastPathComponent()  // Tests
            .deletingLastPathComponent()  // swift/WendyAgentE2ETests
            .deletingLastPathComponent()  // swift
            .deletingLastPathComponent()  // repository root
    }
}
