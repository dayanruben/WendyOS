import Foundation
import Testing
import WendyE2ETesting

extension Machine {
    static func cli() async throws -> Machine {
        let machine = Machine(
            name: "CLI",
            workingDirectory: Self.rootDirectoryURL().appendingPathComponent("go").path
        )

        try! await Gate.buildCLI.once {
            try await machine.run("make build-cli") { standardOutput, _ in
                #expect(standardOutput.contains(/go build .* bin\/wendy/))
            }
        }

        return machine
    }

    static func agent() async throws -> Machine {
        let machine = Machine(
            name: "Agent",
            workingDirectory: Self.rootDirectoryURL().appendingPathComponent("swift").path
        )

        try! await Gate.buildAgent.once {
            try await machine.run("make build-dev") { standardOutput, _ in
                #expect(
                    standardOutput.contains(
                        /Created macOS app artifact: .*wendy-agent-macos-arm64-.*\.zip/
                    )
                )
            }
        }

        return machine
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

private actor Gate {
    static let buildCLI = Gate()
    static let buildAgent = Gate()

    private var done = false

    func once(_ block: () async throws -> Void) async rethrows {
        guard !done else { return }
        done = true
        try await block()
    }
}
