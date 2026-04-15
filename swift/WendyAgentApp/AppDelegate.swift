import AppKit
import WendyAgent

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let agent = WendyAgent()
    private var statusMenuController: StatusMenuController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let statusMenuController = StatusMenuController(agent: self.agent)
        self.statusMenuController = statusMenuController

        Task { @MainActor in
            await statusMenuController.start()
        }
    }
}
