import AppKit
import OSLog
import SwiftUI
import WendyAgentCore

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, StatusMenuControllerDelegate {
    private let logger = Logger(
        subsystem: Bundle.main.bundleIdentifier!,
        category: "AppDelegate"
    )
    private let wendyAgent = WendyAgent()
    private let welcomeAndPermissions = WelcomeAndPermissions()
    private var statusMenuController: StatusMenuController?
    private var welcomeAndPermissionsWindow: NSWindow?
    private var isQuitting = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        self.statusMenuController = StatusMenuController(
            wendyAgent: self.wendyAgent,
            delegate: self
        )

        Task { @MainActor [weak self] in
            guard let self else { return }

            do {
                try await self.wendyAgent.start()
            } catch {
                self.logger.error("Failed to start WendyAgent: \(String(describing: error), privacy: .public)")
            }
        }

        if self.welcomeAndPermissions.shouldShowWelcomeAndPermissions {
            self.showWelcomeAndPermissionsWindow()
        }
    }

    func statusMenuControllerDidSelectWelcomeAndPermissions(_ controller: StatusMenuController) {
        self.showWelcomeAndPermissionsWindow()
    }

    func statusMenuControllerDidSelectQuit(_ controller: StatusMenuController) {
        guard !self.isQuitting else { return }
        self.isQuitting = true

        Task { @MainActor [weak self] in
            guard let self else { return }

            await self.statusMenuController?.invalidate()
            await self.wendyAgent.stop()
            NSApplication.shared.terminate(nil)
        }
    }

    func windowWillClose(_ notification: Notification) {
        guard let window = notification.object as? NSWindow,
              window === self.welcomeAndPermissionsWindow
        else {
            return
        }

        self.welcomeAndPermissionsWindow = nil
    }

    private func makeWelcomeAndPermissionsWindow() -> NSWindow {
        let rootView = WelcomeAndPermissionsView(welcomeAndPermissions: self.welcomeAndPermissions)
        let hostingController = NSHostingController(rootView: rootView)

        let welcomeAndPermissionsWindow = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 620, height: 500),
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false
        )

        welcomeAndPermissionsWindow.contentViewController = hostingController
        welcomeAndPermissionsWindow.delegate = self
        welcomeAndPermissionsWindow.isReleasedWhenClosed = false

        if let closeButton = welcomeAndPermissionsWindow.standardWindowButton(.closeButton) {
            closeButton.keyEquivalent = "w"
            closeButton.keyEquivalentModifierMask = [.command]
        }

        let contentView = welcomeAndPermissionsWindow.contentView!

        contentView.layoutSubtreeIfNeeded()
        let fittingSize = contentView.fittingSize
        let contentSize = NSSize(
            width: max(620, fittingSize.width),
            height: max(320, fittingSize.height)
        )
        welcomeAndPermissionsWindow.setContentSize(contentSize)

        return welcomeAndPermissionsWindow
    }

    private func showWelcomeAndPermissionsWindow() {
        guard self.welcomeAndPermissionsWindow == nil else { return }
        let welcomeAndPermissionsWindow = self.makeWelcomeAndPermissionsWindow()
        self.welcomeAndPermissionsWindow = welcomeAndPermissionsWindow

        self.welcomeAndPermissions.prepareForPresentation()
        NSApplication.shared.activate(ignoringOtherApps: true)
        welcomeAndPermissionsWindow.makeKeyAndOrderFront(nil)
        welcomeAndPermissionsWindow.center()
        welcomeAndPermissionsWindow.setFrameAutosaveName("WelcomeAndPermissionsWindow")
    }
}
