import Foundation

struct AttemptRecordingIdentity: Sendable {
    var suite: String
    var test: String
}

func attemptRecordingIdentity(at attemptURL: URL) -> AttemptRecordingIdentity? {
    let recordingURL = attemptURL.appendingPathComponent("recording.md")
    guard let recording = try? String(contentsOf: recordingURL, encoding: .utf8),
        let suite = recordingHeaderValue("Suite", in: recording),
        let test = recordingHeaderValue("Test", in: recording)
    else {
        return nil
    }
    return AttemptRecordingIdentity(suite: suite, test: test)
}

private func recordingHeaderValue(_ name: String, in recording: String) -> String? {
    let prefix = "- \(name): `"
    for line in recording.split(separator: "\n", omittingEmptySubsequences: false) {
        guard line.hasPrefix(prefix), line.hasSuffix("`") else {
            continue
        }
        return String(line.dropFirst(prefix.count).dropLast())
    }
    return nil
}
