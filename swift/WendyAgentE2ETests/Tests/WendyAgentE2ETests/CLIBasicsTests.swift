import Testing
import WendyE2ETesting

@Suite(.serialized)
struct `CLI basics` {
    var cli: Machine

    init() async throws {
        self.cli = try await Machine.cli()
    }

    @Test
    func `'wendy --help' describes the top-level command groups`() async throws {
        try await self.cli.run("./bin/wendy --help") { standardOutput, standardError in
            #expect(standardError.isEmpty)
            #expect(standardOutput.contains("Project Commands:"))
            #expect(standardOutput.contains("Manage Your Cloud:"))
            #expect(standardOutput.contains("Manage Your Devices:"))
            #expect(standardOutput.contains("Misc.:"))
        }
    }

    @Test
    func `'wendy --version' prints the CLI version`() async throws {
        try await self.cli.run("./bin/wendy --version") { standardOutput, standardError in
            #expect(standardError.isEmpty)
            #expect(standardOutput.contains(/wendy version \S+/))
        }
    }

    @Test
    func `'wendy info' prints CLI and system information`() async throws {
        try await self.cli.run("./bin/wendy info") { standardOutput, standardError in
            #expect(standardError.isEmpty)
            #expect(standardOutput.contains("Wendy CLI"))
            #expect(standardOutput.contains(/Version:\s+\S+/))
            #expect(standardOutput.contains(/OS:\s+\S+/))
            #expect(standardOutput.contains(/Arch:\s+\S+/))
            #expect(standardOutput.contains(/Go Version:\s+\S+/))
        }
    }
}
