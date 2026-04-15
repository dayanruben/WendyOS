import AppKit
import AVFoundation
import CoreBluetooth
import OSLog
import ServiceManagement
import WendyAgentCore

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate, CBCentralManagerDelegate {
    private let logger = Logger(
        subsystem: Bundle.main.bundleIdentifier!,
        category: "AppDelegate"
    )
    private let wendyAgent = WendyAgent()
    private var statusMenuController: StatusMenuController!
    private var bluetoothManager: CBCentralManager?

    func applicationDidFinishLaunching(_ notification: Notification) {
        self.registerForLoginItems()
        self.requestPermissions()

        let statusMenuController = StatusMenuController(wendyAgent: self.wendyAgent)
        self.statusMenuController = statusMenuController

        Task { @MainActor [weak self] in
            guard let self else { return }

            do {
                try await self.wendyAgent.start()
            } catch {
                self.logger.error("Failed to start WendyAgent: \(String(describing: error), privacy: .public)")
            }
        }
    }

    func centralManagerDidUpdateState(_ central: CBCentralManager) {}

    private func registerForLoginItems() {
        let loginItemService = SMAppService.mainApp

        do {
            switch loginItemService.status {
            case .enabled:
                self.logger.info("Wendy Agent is already configured to launch at login")
            case .notRegistered, .requiresApproval, .notFound:
                try loginItemService.register()

                switch loginItemService.status {
                case .enabled:
                    self.logger.info("Configured Wendy Agent to launch at login")
                case .requiresApproval:
                    self.logger.notice("Wendy Agent launch at login requires user approval in System Settings")
                case .notRegistered, .notFound:
                    self.logger.warning("Wendy Agent launch at login registration did not complete; status: \(String(describing: loginItemService.status), privacy: .public)")
                @unknown default:
                    self.logger.warning("Wendy Agent launch at login registration returned an unknown status")
                }
            @unknown default:
                try loginItemService.register()
                self.logger.info("Configured Wendy Agent to launch at login")
            }
        } catch {
            self.logger.error("Failed to configure Wendy Agent to launch at login: \(String(describing: error), privacy: .public)")
        }
    }

    private func requestPermissions() {
        self.bluetoothManager = CBCentralManager(delegate: self, queue: nil)
        AVCaptureDevice.requestAccess(for: .video) { _ in }
        AVCaptureDevice.requestAccess(for: .audio) { _ in }
    }
}
