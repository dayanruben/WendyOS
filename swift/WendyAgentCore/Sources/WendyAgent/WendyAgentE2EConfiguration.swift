public import Foundation

public struct WendyAgentE2EConfiguration {
    private let values: [String: String]

    public static var current: Self? {
        let arguments = ProcessInfo.processInfo.arguments
        guard let index = arguments.firstIndex(of: "--wendy-agent-e2e-config"),
            arguments.indices.contains(arguments.index(after: index))
        else {
            return nil
        }

        let configPath = arguments[arguments.index(after: index)]
        guard isSafeAbsolutePath(configPath) else {
            return nil
        }

        let configURL = URL(fileURLWithPath: configPath, isDirectory: false)
            .standardizedFileURL
            .resolvingSymlinksInPath()
        guard let contents = try? String(contentsOf: configURL, encoding: .utf8) else {
            return nil
        }

        var values: [String: String] = [:]
        for line in contents.split(separator: "\n", omittingEmptySubsequences: true) {
            guard let separator = line.firstIndex(of: "=") else {
                return nil
            }
            let key = String(line[..<separator])
            let value = String(line[line.index(after: separator)...])
            guard key.range(of: #"^[A-Z0-9_]+$"#, options: .regularExpression) != nil else {
                return nil
            }
            values[key] = value
        }

        guard values["WENDY_AGENT_E2E"] == "1" else {
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
        guard url.path == rootURL.path || url.path.hasPrefix(rootURL.path + "/") else {
            return nil
        }
        return url
    }

    private static func isSafeAbsolutePath(_ path: String) -> Bool {
        guard path.range(of: #"^/[-._/A-Za-z0-9]+$"#, options: .regularExpression) != nil else {
            return false
        }
        return !path.split(separator: "/").contains("..")
    }
}
