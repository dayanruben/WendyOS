import Foundation

enum WendyAgentPaths {
    static var stateDirectory: URL {
        #if DEBUG
            if ProcessInfo.processInfo.environment["WENDY_AGENT_E2E"] == "1",
                let stateDirectory = ProcessInfo.processInfo.environment["WENDY_AGENT_STATE_DIR"],
                let e2eRoot = ProcessInfo.processInfo.environment["WENDY_AGENT_E2E_ROOT"],
                !stateDirectory.isEmpty,
                !e2eRoot.isEmpty
            {
                let rootURL = URL(fileURLWithPath: e2eRoot, isDirectory: true)
                    .standardizedFileURL
                    .resolvingSymlinksInPath()
                let stateURL = URL(fileURLWithPath: stateDirectory, isDirectory: true)
                    .standardizedFileURL
                    .resolvingSymlinksInPath()
                if stateURL.path == rootURL.path || stateURL.path.hasPrefix(rootURL.path + "/") {
                    return stateURL
                }
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
