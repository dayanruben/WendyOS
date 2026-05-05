import Foundation
import Testing
import WendyE2ETesting

@Suite(.serialized)
struct `wendy analytics` {
    var cli: Machine

    init() async throws {
        self.cli = try await Machine.cli()
    }

    @Test
    func `'wendy analytics status' shows whether analytics are enabled`() async throws {
        let home = try Helper.temporaryDirectory(prefix: "wendy-analytics-status")
        defer { try? FileManager.default.removeItem(at: home) }

        try Helper.writeAnalyticsConfig(enabled: true, home: home)
        try await self.cli.run("HOME=\(Helper.shellQuote(home.path)) ./bin/wendy analytics status")
        {
            standardOutput,
            standardError in
            #expect(standardOutput.isEmpty)
            #expect(standardError == "Analytics: enabled\n")
        }

        try Helper.writeAnalyticsConfig(enabled: false, home: home)
        try await self.cli.run("HOME=\(Helper.shellQuote(home.path)) ./bin/wendy analytics status")
        {
            standardOutput,
            standardError in
            #expect(standardOutput.isEmpty)
            #expect(standardError == "Analytics: disabled\n")
        }
    }

    @Test
    func `'wendy analytics enable' enables anonymous analytics`() async throws {
        let home = try Helper.temporaryDirectory(prefix: "wendy-analytics-enable")
        defer { try? FileManager.default.removeItem(at: home) }
        try Helper.writeAnalyticsConfig(enabled: false, home: home)

        try await self.cli.run("HOME=\(Helper.shellQuote(home.path)) ./bin/wendy analytics enable")
        {
            standardOutput,
            standardError in
            #expect(standardOutput.isEmpty)
            #expect(standardError == "Analytics enabled.\n")
        }

        #expect(try Helper.analyticsConfigEnabled(home: home) == true)
        try await self.cli.run("HOME=\(Helper.shellQuote(home.path)) ./bin/wendy analytics status")
        {
            standardOutput,
            standardError in
            #expect(standardOutput.isEmpty)
            #expect(standardError == "Analytics: enabled\n")
        }
    }

    @Test
    func `'wendy analytics disable' disables anonymous analytics`() async throws {
        let home = try Helper.temporaryDirectory(prefix: "wendy-analytics-disable")
        defer { try? FileManager.default.removeItem(at: home) }
        try Helper.writeAnalyticsConfig(enabled: true, home: home)

        try await self.cli.run("HOME=\(Helper.shellQuote(home.path)) ./bin/wendy analytics disable")
        {
            standardOutput,
            standardError in
            #expect(standardOutput.isEmpty)
            #expect(standardError == "Analytics disabled.\n")
        }

        #expect(try Helper.analyticsConfigEnabled(home: home) == false)
        try await self.cli.run("HOME=\(Helper.shellQuote(home.path)) ./bin/wendy analytics status")
        {
            standardOutput,
            standardError in
            #expect(standardOutput.isEmpty)
            #expect(standardError == "Analytics: disabled\n")
        }
    }

    // MARK: -

    @Suite
    struct `wendy analytics disable` {
        // TODO: implement.
    }

    // MARK: -

    @Suite
    struct `wendy analytics enable` {
        // TODO: implement.
    }

    // MARK: -

    @Suite
    struct `wendy analytics status` {
        // TODO: implement.
    }

}
