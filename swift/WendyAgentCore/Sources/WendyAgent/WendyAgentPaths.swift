import Foundation

enum WendyAgentPaths {
    static var stateDirectory: URL {
        #if DEBUG
            if let e2eConfiguration = WendyAgentE2EConfiguration.current {
                guard let stateURL = e2eConfiguration.urlInsideRoot(
                    for: "WENDY_AGENT_STATE_DIR",
                    isDirectory: true
                ) else {
                    fatalError("Invalid WendyAgentMac E2E state directory configuration")
                }
                return stateURL
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
