public import Foundation

public struct WendyAgentE2EConfiguration {
    private static let expectedKeys: Set<String> = [
        "WENDY_AGENT_E2E",
        "WENDY_AGENT_PORT",
        "WENDY_AGENT_STATE_DIR",
        "WENDY_AGENT_E2E_ROOT",
        "WENDY_AGENT_E2E_PID_FILE",
        "WENDY_OTEL_PORT",
    ]

    private let values: [String: String]

    public static var current: Self? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let index = arguments.firstIndex(of: "--wendy-agent-e2e-config"),
            arguments.indices.contains(arguments.index(after: index))
        else {
            return nil
        }

        let configPath = arguments[arguments.index(after: index)]
        guard let configURL = safeResolvedRegularFileURL(configPath),
            let contents = try? String(contentsOf: configURL, encoding: .utf8)
        else {
            return nil
        }

        var values: [String: String] = [:]
        for line in contents.split(separator: "\n", omittingEmptySubsequences: true) {
            guard !line.contains("\r"), let separator = line.firstIndex(of: "=") else {
                return nil
            }
            let key = String(line[..<separator])
            let value = String(line[line.index(after: separator)...])
            guard expectedKeys.contains(key), values[key] == nil, isSafeValue(value) else {
                return nil
            }
            values[key] = value
        }

        guard Set(values.keys) == expectedKeys,
            values["WENDY_AGENT_E2E"] == "1",
            isValidPort(values["WENDY_AGENT_PORT"], allowsZero: false),
            isValidPort(values["WENDY_OTEL_PORT"], allowsZero: true),
            isSafeAbsolutePath(values["WENDY_AGENT_STATE_DIR"]),
            isSafeAbsolutePath(values["WENDY_AGENT_E2E_ROOT"]),
            isSafeAbsolutePath(values["WENDY_AGENT_E2E_PID_FILE"])
        else {
            return nil
        }
        return Self(values: values)
    }

    public subscript(_ key: String) -> String? {
        self.values[key]
    }

    public func urlInsideRoot(for key: String, isDirectory: Bool) -> URL? {
        guard let rootPath = self.values["WENDY_AGENT_E2E_ROOT"],
            let path = self.values[key],
            Self.isSafeAbsolutePath(rootPath),
            Self.isSafeAbsolutePath(path)
        else {
            return nil
        }

        let rootURL = URL(fileURLWithPath: rootPath, isDirectory: true)
            .standardizedFileURL
            .resolvingSymlinksInPath()
        let url = URL(fileURLWithPath: path, isDirectory: isDirectory)
            .standardizedFileURL
            .resolvingSymlinksInPath()
        guard Self.isSafeAbsolutePath(rootURL.path),
            Self.isSafeAbsolutePath(url.path),
            url.path == rootURL.path || url.path.hasPrefix(rootURL.path + "/")
        else {
            return nil
        }
        return url
    }

    private static func safeResolvedRegularFileURL(_ path: String) -> URL? {
        guard isSafeAbsolutePath(path) else {
            return nil
        }

        let literalURL = URL(fileURLWithPath: path, isDirectory: false).standardizedFileURL
        let resolvedURL = literalURL.resolvingSymlinksInPath()
        guard isSafeAbsolutePath(resolvedURL.path), literalURL.path == resolvedURL.path else {
            return nil
        }

        guard let values = try? resolvedURL.resourceValues(forKeys: [.isRegularFileKey]),
            values.isRegularFile == true
        else {
            return nil
        }
        return resolvedURL
    }

    private static func isSafeAbsolutePath(_ path: String?) -> Bool {
        guard let path else {
            return false
        }
        guard path.range(of: #"^/[-._/@ A-Za-z0-9]+$"#, options: .regularExpression) != nil else {
            return false
        }
        return !path.split(separator: "/").contains("..")
    }

    private static func isSafeValue(_ value: String) -> Bool {
        !value.contains("\n") && !value.contains("\r")
    }

    private static func isValidPort(_ value: String?, allowsZero: Bool) -> Bool {
        guard let value,
            value.range(of: #"^[0-9]{1,5}$"#, options: .regularExpression) != nil,
            let port = Int(value)
        else {
            return false
        }
        let lowerBound = allowsZero ? 0 : 1
        return (lowerBound...65535).contains(port)
    }
}
