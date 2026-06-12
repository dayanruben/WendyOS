import Foundation

let e2eAttemptArtifactsDirectoryName = "attempts"
let e2eObservationsDirectoryName = "observations"

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
