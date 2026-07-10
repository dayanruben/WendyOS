import Foundation

/// Small shared helper for running system tools (`system_profiler`, `networksetup`,
/// `scutil`, `lsof`, …) from the macOS agent's capability providers.
///
/// Output is captured with a byte cap to bound memory, and the process is
/// terminated (then force-killed) if it overruns the timeout.
enum Subprocess {
    struct Result: Sendable {
        var status: Int32
        var stdout: String
        var stderr: String
    }

    /// Maximum number of bytes captured per stream before truncation.
    static let maxOutputBytes = 256 * 1024

    static func run(
        _ executable: String,
        _ arguments: [String],
        timeout: Duration = .seconds(10)
    ) async throws -> Result {
        try await Task.detached(priority: .utility) {
            let process = Foundation.Process()
            process.executableURL = URL(fileURLWithPath: executable)
            process.arguments = arguments

            let stdoutPipe = Pipe()
            let stderrPipe = Pipe()
            process.standardOutput = stdoutPipe
            process.standardError = stderrPipe

            try process.run()

            // Read both pipes concurrently so a full pipe buffer can never
            // deadlock the child process.
            async let stdoutData = Self.readCapped(stdoutPipe.fileHandleForReading)
            async let stderrData = Self.readCapped(stderrPipe.fileHandleForReading)

            let deadlineSeconds =
                Double(timeout.components.seconds)
                + Double(timeout.components.attoseconds) / 1e18
            let deadline = Date().addingTimeInterval(deadlineSeconds)
            var timedOut = false
            while process.isRunning {
                if Date() >= deadline {
                    timedOut = true
                    process.terminate()
                    let graceDeadline = Date().addingTimeInterval(2)
                    while process.isRunning && Date() < graceDeadline {
                        try await Task.sleep(for: .milliseconds(50))
                    }
                    if process.isRunning {
                        kill(process.processIdentifier, SIGKILL)
                    }
                    process.waitUntilExit()
                    break
                }
                try await Task.sleep(for: .milliseconds(25))
            }

            let out = String(decoding: await stdoutData, as: UTF8.self)
            let err = String(decoding: await stderrData, as: UTF8.self)
            return Result(
                status: timedOut ? 124 : process.terminationStatus,
                stdout: out,
                stderr: err
            )
        }.value
    }

    private static func readCapped(_ handle: FileHandle) -> Data {
        var data = Data()
        while data.count <= maxOutputBytes {
            let chunk = handle.availableData
            if chunk.isEmpty { break }
            data.append(chunk)
        }
        if data.count > maxOutputBytes {
            data = data.prefix(maxOutputBytes)
        }
        return data
    }
}
