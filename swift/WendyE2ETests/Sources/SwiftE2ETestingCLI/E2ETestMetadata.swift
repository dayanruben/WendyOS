import ArgumentParser
import Foundation

let e2eTestMetadataFileName = "test.json"
let e2eTestMetadataSchemaID = "wendy.e2e.test.v1"

struct E2ETestMetadata: Codable, Sendable {
    var schema: String
    var sourceFilePath: String
    var sourceFileName: String
    var suiteName: String
    var testName: String
    var functionName: String
    var line: Int

    var identityKey: E2ETestIdentityKey {
        E2ETestIdentityKey(
            sourceFilePath: sourceFilePath,
            suiteName: suiteName,
            testName: testName
        )
    }
}

struct E2ETestIdentityKey: Hashable, Sendable {
    var sourceFilePath: String
    var suiteName: String
    var testName: String
}

func loadE2ETestMetadata(in observationURL: URL) throws -> E2ETestMetadata {
    let url = observationURL.appendingPathComponent(e2eTestMetadataFileName)
    let data = try Data(contentsOf: url)
    let metadata = try JSONDecoder().decode(E2ETestMetadata.self, from: data)
    guard metadata.schema == e2eTestMetadataSchemaID else {
        throw ValidationError("Test metadata has unsupported schema: \(url.path)")
    }
    return metadata
}

func firstE2ETestMetadata(in testObservationURL: URL) throws -> E2ETestMetadata? {
    let directURL = testObservationURL.appendingPathComponent(e2eTestMetadataFileName)
    if FileManager.default.fileExists(atPath: directURL.path) {
        return try loadE2ETestMetadata(in: testObservationURL)
    }

    for targetURL in try e2eMetadataDirectoryChildren(of: testObservationURL) {
        for attemptURL in try e2eMetadataDirectoryChildren(of: targetURL) {
            let metadataURL = attemptURL.appendingPathComponent(e2eTestMetadataFileName)
            if FileManager.default.fileExists(atPath: metadataURL.path) {
                return try loadE2ETestMetadata(in: attemptURL)
            }
        }
    }

    return nil
}

func e2ePackageRelativePath(from packageURL: URL, to url: URL) -> String {
    let basePath = packageURL.standardizedFileURL.path
    let path = url.standardizedFileURL.path
    let prefix = basePath.hasSuffix("/") ? basePath : basePath + "/"
    guard path.hasPrefix(prefix) else {
        return url.lastPathComponent
    }
    return String(path.dropFirst(prefix.count))
}

private func e2eMetadataDirectoryChildren(of url: URL) throws -> [URL] {
    guard FileManager.default.fileExists(atPath: url.path) else { return [] }
    return try FileManager.default.contentsOfDirectory(
        at: url,
        includingPropertiesForKeys: [.isDirectoryKey],
        options: [.skipsHiddenFiles]
    )
    .filter { (try? $0.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true }
    .sorted { $0.path < $1.path }
}
