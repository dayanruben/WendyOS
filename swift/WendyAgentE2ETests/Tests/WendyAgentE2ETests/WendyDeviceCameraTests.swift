import Testing
import WendyE2ETesting

@Suite(.serialized)
struct `wendy device camera` {
    var cli: Machine
    init() async throws { self.cli = try await Machine.cli() }

    @Test
    func `describes subcommands`() async throws {
        try await self.cli.run("./bin/wendy device camera --help") { standardOutput, standardError in
            #expect(standardError.isEmpty)
            #expect(standardOutput.contains("Manage cameras"))
            #expect(standardOutput.contains("list"))
            #expect(standardOutput.contains("view"))
        }
    }
}

// MARK: -

@Suite(.serialized)
struct `wendy device camera list` {
    var cli: Machine
    init() async throws { self.cli = try await Machine.cli() }

    @Test
    func `lists cameras on the selected device`() async throws {
        let record = try await self.cli.run("WENDY_ANALYTICS=false ./bin/wendy --device 127.0.0.1 device camera list", output: .string(limit: .max), error: .string(limit: .max))
        #expect(record.terminationStatus.isSuccess)
        #expect(record.standardOutput?.contains("Camera") == true || record.standardOutput?.contains("No cameras") == true)
    }

    @Test
    func `'--json' formats cameras as JSON`() async throws {
        let record = try await self.cli.run("WENDY_ANALYTICS=false ./bin/wendy --json --device 127.0.0.1 device camera list", output: .string(limit: .max), error: .string(limit: .max))
        #expect(record.terminationStatus.isSuccess)
        let array = try Helper.jsonArray(from: record.standardOutput ?? "")
        if let first = array.first as? [String: Any] {
            #expect(first["id"] != nil)
            #expect(first["name"] as? String != nil)
        }
    }
}

// MARK: -

@Suite(.serialized)
struct `wendy device camera view` {
    var cli: Machine
    init() async throws { self.cli = try await Machine.cli() }

    @Test
    func `opens a camera viewer for the selected camera`() async throws {
        let record = try await self.cli.run("WENDY_ANALYTICS=false /usr/bin/perl -e 'alarm 2; exec @ARGV' ./bin/wendy --device 127.0.0.1 device camera view --id 1 --stdout --width 320 --height 240 --fps 5", output: .string(limit: .max), error: .string(limit: .max))
        #expect(record.terminationStatus.isSuccess)
        #expect(record.standardOutput?.isEmpty == false || record.standardError?.contains("camera") == true)
    }

    @Test
    func `fails clearly when the selected camera is unavailable`() async throws {
        let record = try await self.cli.run("WENDY_ANALYTICS=false /usr/bin/perl -e 'alarm 2; exec @ARGV' ./bin/wendy --device 127.0.0.1 device camera view --id 999 --stdout", output: .string(limit: .max), error: .string(limit: .max))
        #expect(!record.terminationStatus.isSuccess)
        #expect(record.standardError?.contains("999") == true || record.standardError?.contains("camera") == true || record.standardError?.contains("Could not connect") == true)
    }
}
