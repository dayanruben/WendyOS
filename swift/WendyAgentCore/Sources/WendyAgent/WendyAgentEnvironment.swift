import Foundation

public enum WendyAgentEnvironment {
    public static var host: String? {
        value("WENDY_AGENT_HOST")
    }

    public static var port: Int? {
        port("WENDY_AGENT_PORT", minimum: 1)
    }

    public static var otelPort: Int? {
        port("WENDY_OTEL_PORT", minimum: 0)
    }

    public static var appPath: String? {
        value("WENDY_AGENT_APP_PATH")
    }

    public static var sandboxProfile: String? {
        value("WENDY_AGENT_SANDBOX_PROFILE")
    }

    private static func value(_ name: String) -> String? {
        guard let value = ProcessInfo.processInfo.environment[name], !value.isEmpty else {
            return nil
        }
        return value
    }

    private static func port(_ name: String, minimum: Int) -> Int? {
        guard let value = value(name),
            let port = Int(value),
            (minimum...65_535).contains(port)
        else {
            return nil
        }
        return port
    }
}
