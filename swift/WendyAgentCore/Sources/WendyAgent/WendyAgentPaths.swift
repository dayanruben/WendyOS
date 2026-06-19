import Foundation

enum WendyAgentPaths {
    static var stateDirectory: URL {
        #if DEBUG
            if let stateDirectory = ProcessInfo.processInfo.environment["WENDY_AGENT_STATE_DIR"],
                !stateDirectory.isEmpty
            {
                return URL(fileURLWithPath: stateDirectory, isDirectory: true)
            }
        #endif

        return self.applicationSupportDirectory.appendingPathComponent(
            self.bundleIdentifierComponent,
            isDirectory: true
        )
    }

    private static var applicationSupportDirectory: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support", isDirectory: true)
    }

    private static var bundleIdentifierComponent: String {
        if let bundleIdentifier = Bundle.main.bundleIdentifier, !bundleIdentifier.isEmpty {
            return bundleIdentifier
        }

        return ProcessInfo.processInfo.processName
    }
}
