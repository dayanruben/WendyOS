import ArgumentParser
import Foundation

struct AggregateCommand: ParsableCommand {
    static let configuration = CommandConfiguration(
        commandName: "aggregate",
        abstract: "Aggregate Swift E2E attempts into a run layout."
    )

    @Option(
        name: .long,
        help: "Directory where the E2E run is written. Defaults to the first attempt's parent."
    )
    var outputDir: String?

    @Argument(help: "Swift E2E attempt directories to aggregate.")
    var attemptDirs: [String] = []

    mutating func run() throws {
        guard !attemptDirs.isEmpty else {
            throw ValidationError("Missing attempt directory.")
        }

        let attemptURLs = attemptDirs.map {
            URL(fileURLWithPath: $0, isDirectory: true).standardizedFileURL
        }
        let firstAttemptURL = attemptURLs[0]
        let outputURL = URL(
            fileURLWithPath: outputDir ?? firstAttemptURL.deletingLastPathComponent().path,
            isDirectory: true
        ).standardizedFileURL

        try FileManager.default.createDirectory(at: outputURL, withIntermediateDirectories: true)

        var runURLs: Set<URL> = []
        var mappedAttempts: [MappedAttempt] = []
        for attemptURL in attemptURLs {
            let attemptID = attemptURL.lastPathComponent
            let components = try AttemptID(attemptID)
            let runURL = outputURL.appendingPathComponent(
                "\(components.workflowName).\(components.runID)",
                isDirectory: true
            )
            runURLs.insert(runURL)
            try FileManager.default.createDirectory(
                at: runURL,
                withIntermediateDirectories: true
            )

            let mappedAttempt = try mapAttempt(
                attemptURL: attemptURL,
                attemptID: attemptID,
                components: components,
                runURL: runURL
            )
            mappedAttempts.append(mappedAttempt)
        }

        for runURL in runURLs {
            let runAttempts = mappedAttempts.filter { mappedAttempt in
                mappedAttempt.runURL == runURL
            }
            try writeRunStructureInfo(at: runURL, mappedAttempts: runAttempts)
            try writeAggregateInfo(at: runURL, mappedAttempts: runAttempts)
        }

        for root in runURLs.sorted(by: { $0.path < $1.path }) {
            print("==> Wrote Swift E2E run: \(root.path)")
        }
    }

    private func mapAttempt(
        attemptURL: URL,
        attemptID: String,
        components: AttemptID,
        runURL: URL
    ) throws -> MappedAttempt {
        guard FileManager.default.fileExists(atPath: attemptURL.path) else {
            throw ValidationError("Attempt directory does not exist: \(attemptURL.path)")
        }

        let testsURL = attemptURL.appendingPathComponent("tests", isDirectory: true)
        let testDirectories = try attemptTestDirectories(in: testsURL)
        var mappedTests: [MappedTest] = []
        for testDirectory in testDirectories {
            let suiteKey = testDirectory.deletingLastPathComponent().lastPathComponent
            let testKey = testDirectory.lastPathComponent
            let destinationURL =
                runURL
                .appendingPathComponent(suiteKey, isDirectory: true)
                .appendingPathComponent(testKey, isDirectory: true)
                .appendingPathComponent(components.targetName, isDirectory: true)
                .appendingPathComponent(components.attempt, isDirectory: true)

            try? FileManager.default.removeItem(at: destinationURL)
            try FileManager.default.createDirectory(
                at: destinationURL.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            try copyItem(at: testDirectory, to: destinationURL)
            try copyAttemptLevelFiles(from: attemptURL, to: destinationURL)
            mappedTests.append(
                MappedTest(
                    suiteKey: suiteKey,
                    testKey: testKey,
                    path: runRelativePath(destinationURL, base: runURL)
                )
            )
        }

        return MappedAttempt(
            attemptID: attemptID,
            runURL: runURL,
            workflowName: components.workflowName,
            workflowRunID: components.runID,
            targetName: components.targetName,
            attempt: components.attempt,
            mappedTests: mappedTests
        )
    }
}

private struct AttemptID {
    var workflowName: String
    var runID: String
    var targetName: String
    var attempt: String

    init(_ value: String) throws {
        let parts = value.split(separator: ".", omittingEmptySubsequences: false).map(String.init)
        guard parts.count >= 4 else {
            throw ValidationError(
                "Attempt ID must have shape <workflow-name>.<run-id>.<target-name>.<attempt>: \(value)"
            )
        }
        self.workflowName = parts[0]
        self.runID = parts[1]
        self.targetName = parts.dropFirst(2).dropLast().joined(separator: ".")
        self.attempt = parts[parts.count - 1]
        guard !workflowName.isEmpty, !runID.isEmpty, !targetName.isEmpty, !attempt.isEmpty else {
            throw ValidationError("Attempt ID contains an empty component: \(value)")
        }
    }
}

private struct MappedAttempt: Encodable {
    var attemptID: String
    var runURL: URL
    var workflowName: String
    var workflowRunID: String
    var targetName: String
    var attempt: String
    var mappedTests: [MappedTest]

    enum CodingKeys: String, CodingKey {
        case attemptID = "attemptId"
        case workflowName
        case workflowRunID = "runId"
        case targetName
        case attempt
        case mappedTests
    }
}

private struct MappedTest: Encodable {
    var suiteKey: String
    var testKey: String
    var path: String
}

private struct AggregateInfo: Encodable {
    var kind: String
    var version: Int
    var generatedAt: String
    var attempts: [MappedAttempt]
}

private struct SuiteInfo: Encodable {
    var kind: String
    var version: Int
    var suiteKey: String
    var tests: [String]
}

private struct TestInfo: Encodable {
    var kind: String
    var version: Int
    var suiteKey: String
    var testKey: String
    var attempts: [TestAttemptInfo]
}

private struct TestAttemptInfo: Encodable {
    var attemptID: String
    var targetName: String
    var attempt: String
    var path: String

    enum CodingKeys: String, CodingKey {
        case attemptID = "attemptId"
        case targetName
        case attempt
        case path
    }
}

private func attemptTestDirectories(in testsURL: URL) throws -> [URL] {
    guard FileManager.default.fileExists(atPath: testsURL.path) else {
        return []
    }
    guard
        let enumerator = FileManager.default.enumerator(
            at: testsURL,
            includingPropertiesForKeys: [.isDirectoryKey]
        )
    else {
        throw ValidationError("Tests directory cannot be read: \(testsURL.path)")
    }

    var directories: [URL] = []
    for case let url as URL in enumerator {
        let recordURL = url.appendingPathComponent("recording.md")
        if FileManager.default.fileExists(atPath: recordURL.path) {
            directories.append(url)
        }
    }
    return directories.sorted { $0.path < $1.path }
}

private func copyAttemptLevelFiles(from attemptURL: URL, to destinationURL: URL) throws {
    for fileName in ["attempt.json", "test-results.xml", "test-results.raw.xml"] {
        let sourceURL = attemptURL.appendingPathComponent(fileName)
        guard FileManager.default.fileExists(atPath: sourceURL.path) else { continue }
        try copyItem(at: sourceURL, to: destinationURL.appendingPathComponent(fileName))
    }
}

private func copyItem(at sourceURL: URL, to destinationURL: URL) throws {
    if FileManager.default.fileExists(atPath: destinationURL.path) {
        try FileManager.default.removeItem(at: destinationURL)
    }
    try FileManager.default.copyItem(at: sourceURL, to: destinationURL)
}

private func writeRunStructureInfo(at runURL: URL, mappedAttempts: [MappedAttempt]) throws {
    var testAttempts: [RunTestKey: [TestAttemptInfo]] = [:]
    for mappedAttempt in mappedAttempts {
        for test in mappedAttempt.mappedTests {
            let key = RunTestKey(suiteKey: test.suiteKey, testKey: test.testKey)
            testAttempts[key, default: []].append(
                TestAttemptInfo(
                    attemptID: mappedAttempt.attemptID,
                    targetName: mappedAttempt.targetName,
                    attempt: mappedAttempt.attempt,
                    path: test.path
                )
            )
        }
    }

    let testsBySuite = Dictionary(grouping: testAttempts.keys, by: \.suiteKey)
    for suiteKey in testsBySuite.keys.sorted() {
        let testKeys = testsBySuite[suiteKey, default: []].map(\.testKey).sorted()
        let suiteURL = runURL.appendingPathComponent(suiteKey, isDirectory: true)
        try writeJSON(
            SuiteInfo(
                kind: "swift-e2e-suite",
                version: 1,
                suiteKey: suiteKey,
                tests: testKeys
            ),
            to: suiteURL.appendingPathComponent("suite.json")
        )

        for testKey in testKeys {
            let testURL = suiteURL.appendingPathComponent(testKey, isDirectory: true)
            let attempts = testAttempts[
                RunTestKey(suiteKey: suiteKey, testKey: testKey),
                default: []
            ].sorted {
                if $0.targetName != $1.targetName { return $0.targetName < $1.targetName }
                return $0.attempt < $1.attempt
            }
            try writeJSON(
                TestInfo(
                    kind: "swift-e2e-test",
                    version: 1,
                    suiteKey: suiteKey,
                    testKey: testKey,
                    attempts: attempts
                ),
                to: testURL.appendingPathComponent("test.json")
            )
        }
    }
}

private struct RunTestKey: Hashable {
    var suiteKey: String
    var testKey: String
}

private func writeAggregateInfo(at runURL: URL, mappedAttempts: [MappedAttempt]) throws {
    try writeJSON(
        AggregateInfo(
            kind: "swift-e2e-aggregate",
            version: 1,
            generatedAt: ISO8601DateFormatter().string(from: Date()),
            attempts: mappedAttempts.sorted { $0.attemptID < $1.attemptID }
        ),
        to: runURL.appendingPathComponent("aggregate.json")
    )
}

private func writeJSON<T: Encodable>(_ value: T, to url: URL) throws {
    let encoder = JSONEncoder()
    encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
    let data = try encoder.encode(value)
    try data.write(to: url, options: .atomic)
}

private func runRelativePath(_ url: URL, base: URL) -> String {
    let basePath = base.standardizedFileURL.path
    let path = url.standardizedFileURL.path
    let prefix = basePath.hasSuffix("/") ? basePath : basePath + "/"
    guard path.hasPrefix(prefix) else {
        return url.lastPathComponent
    }
    return String(path.dropFirst(prefix.count))
}
