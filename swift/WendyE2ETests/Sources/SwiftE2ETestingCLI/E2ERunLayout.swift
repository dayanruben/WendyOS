import Foundation

#if canImport(FoundationXML)
    import FoundationXML
#endif

let e2eAttemptArtifactsDirectoryName = "attempts"
let e2eObservationsDirectoryName = "observations"
let e2eSourceArtifactFileName = "source.md"
let e2eSourceIndexFileName = "source-index.md"

func e2eAttemptArtifactsRootURL(in runURL: URL) -> URL {
    runURL.appendingPathComponent(e2eAttemptArtifactsDirectoryName, isDirectory: true)
}

func e2eObservationsRootURL(in runURL: URL) -> URL {
    runURL.appendingPathComponent(e2eObservationsDirectoryName, isDirectory: true)
}

func e2eAttemptArtifactsURL(in runURL: URL, targetName: String, attempt: String) -> URL {
    e2eAttemptArtifactsRootURL(in: runURL)
        .appendingPathComponent(targetName, isDirectory: true)
        .appendingPathComponent(attempt, isDirectory: true)
}

func e2eAttemptExitStatus(at attemptURL: URL) -> Int? {
    guard let object = e2eAttemptJSON(at: attemptURL) else { return nil }
    return object["exitStatus"] as? Int
}

func e2eAttemptXUnitURL(at attemptURL: URL) -> URL? {
    let normalizedURL = attemptURL.appendingPathComponent("test-results.xml")
    if FileManager.default.fileExists(atPath: normalizedURL.path) {
        return normalizedURL
    }

    let swiftTestingURL = attemptURL.appendingPathComponent("test-results-swift-testing.xml")
    if FileManager.default.fileExists(atPath: swiftTestingURL.path) {
        return swiftTestingURL
    }

    return nil
}

func e2eAttemptInfrastructureFailureDetail(at attemptURL: URL) -> String? {
    if let timeout = e2eAttemptTimeoutDetail(at: attemptURL) {
        return timeout
    }

    let xunitProblem = e2eAttemptXUnitProblem(at: attemptURL)
    if let exitStatus = e2eAttemptExitStatus(at: attemptURL), exitStatus != 0 {
        if let xunitProblem {
            return "attempt exited with status \(exitStatus) and produced unusable xUnit output: \(xunitProblem)"
        }
        if e2eAttemptXUnitURL(at: attemptURL) == nil {
            return "attempt exited with status \(exitStatus) before producing test-results.xml"
        }
    }

    if let xunitProblem {
        return "attempt produced unusable xUnit output: \(xunitProblem)"
    }

    return nil
}

private func e2eAttemptJSON(at attemptURL: URL) -> [String: Any]? {
    let url = attemptURL.appendingPathComponent("attempt.json")
    guard FileManager.default.fileExists(atPath: url.path),
        let data = try? Data(contentsOf: url),
        let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
    else {
        return nil
    }
    return object
}

private func e2eAttemptTimeoutDetail(at attemptURL: URL) -> String? {
    let url = attemptURL.appendingPathComponent("timeout.json")
    guard FileManager.default.fileExists(atPath: url.path) else { return nil }
    if let data = try? Data(contentsOf: url),
        let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
        let message = object["message"] as? String,
        !message.isEmpty
    {
        return message
    }
    return "Swift E2E attempt timed out"
}

private func e2eAttemptXUnitProblem(at attemptURL: URL) -> String? {
    guard let url = e2eAttemptXUnitURL(at: attemptURL) else { return nil }
    guard let data = try? Data(contentsOf: url) else {
        return "could not read \(url.lastPathComponent)"
    }
    guard !data.isEmpty else {
        return "\(url.lastPathComponent) is empty"
    }

    let parser = XMLParser(data: data)
    if parser.parse() {
        return nil
    }

    return "\(url.lastPathComponent) is truncated or not parseable"
}
