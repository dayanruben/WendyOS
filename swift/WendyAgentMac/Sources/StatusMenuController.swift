import AppKit
import WendyAgentCore

@MainActor
final class StatusMenuController: NSObject {
    let wendyAgent: WendyAgent

    init(wendyAgent: WendyAgent, bundle: Bundle = .main) {
        self.wendyAgent = wendyAgent
        self.bundleDisplayName = AppDisplayName.resolve(from: bundle)
        self.currentStatus = wendyAgent.status
        self.statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        super.init()

        self.statusObservation = self.wendyAgent.observeStatus { @MainActor [weak self] status in
            self?.update(status: status)
        }

        self.statusItem.isVisible = true
        self.updateStatusButton()
        self.rebuildMenu()
    }

    private let bundleDisplayName: String
    private let statusItem: NSStatusItem
    private var currentStatus: WendyAgentStatus
    private var statusObservation: WendyObservation?
    private var isQuitting = false

    private func update(status: WendyAgentStatus) {
        self.currentStatus = status
        self.updateStatusButton()
        self.rebuildMenu()
    }

    private func rebuildMenu() {
        let menu = NSMenu()
        menu.autoenablesItems = false

        let statusItem = NSMenuItem(
            title: self.currentStatus.menuTitle,
            action: nil,
            keyEquivalent: ""
        )
        statusItem.image = self.makeStatusImage(for: self.currentStatus)
        statusItem.isEnabled = false
        menu.addItem(statusItem)

        for detail in self.currentStatus.menuFailureDetails {
            let detailItem = NSMenuItem(title: detail, action: nil, keyEquivalent: "")
            detailItem.isEnabled = false
            menu.addItem(detailItem)
        }

        menu.addItem(.separator())

        let quitItem = NSMenuItem(
            title: "Quit \(self.bundleDisplayName)",
            action: #selector(self.quitSelected),
            keyEquivalent: "q"
        )
        quitItem.target = self
        menu.addItem(quitItem)

        self.statusItem.menu = menu
    }

    private func updateStatusButton() {
        guard let button = self.statusItem.button else { return }

        let image = self.makeButtonImage(for: self.currentStatus)
        image?.isTemplate = true

        button.image = image
        button.title = self.buttonTitle(for: self.currentStatus, image: image)
        button.imagePosition = self.buttonImagePosition(for: self.currentStatus, image: image)
        button.imageScaling = .scaleProportionallyDown
        button.toolTip = "\(self.bundleDisplayName) — \(self.currentStatus.menuTitle)"
        button.setAccessibilityTitle(self.bundleDisplayName)
    }

    private func buttonTitle(for status: WendyAgentStatus, image: NSImage?) -> String {
        if case .failed = status {
            return "!"
        }

        return image == nil ? "W" : ""
    }

    private func buttonImagePosition(for status: WendyAgentStatus, image: NSImage?) -> NSControl.ImagePosition {
        guard image != nil else {
            return .noImage
        }

        if case .failed = status {
            return .imageLeading
        }

        return .imageOnly
    }

    private func makeButtonImage(for status: WendyAgentStatus) -> NSImage? {
        if let image = NSImage(named: NSImage.Name("StatusIcon"))?.copy() as? NSImage {
            return image
        }

        return NSImage(
            systemSymbolName: "diamond.fill",
            accessibilityDescription: self.bundleDisplayName
        )
    }

    private func makeStatusImage(for status: WendyAgentStatus) -> NSImage? {
        guard let image = NSImage(named: NSImage.Name(status.menuImageName))?.copy() as? NSImage else {
            return nil
        }

        image.isTemplate = false
        return image
    }

    @objc
    private func quitSelected() {
        guard !self.isQuitting else { return }
        self.isQuitting = true

        Task { @MainActor in
            await self.cancelStatusObservation()
            await self.wendyAgent.stop()
            NSApplication.shared.terminate(nil)
        }
    }

    private func cancelStatusObservation() async {
        guard let statusObservation = self.statusObservation else { return }
        self.statusObservation = nil
        await statusObservation.cancel()
    }
}
