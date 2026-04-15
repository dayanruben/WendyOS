import AppKit
import WendyAgent

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let wendyAgent = WendyAgent()
    private var statusMenuController: StatusMenuController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let statusMenuController = StatusMenuController(wendyAgent: self.wendyAgent)
        self.statusMenuController = statusMenuController

        Task { @MainActor in
            await statusMenuController.start()
        }
    }
}
