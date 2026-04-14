import AppKit
import SwiftUI
import WendyAgent

@main
struct WendyAgentApp: App {
    @State private var status: WendyAgentStatus = .idle
    @State private var statusObservation: WendyObservation?
    @State private var hasBootstrapped = false
    @State private var isQuitting = false

    private let agent = WendyAgent()

    var body: some Scene {
        MenuBarExtra {
            WendyAgentMenu(status: self.status, onQuit: self.quit)
        } label: {
            WendyAgentStatusItem(status: self.status)
                .task {
                    await self.bootstrapIfNeeded()
                }
        }
    }

    @MainActor
    private func bootstrapIfNeeded() async {
        guard !self.hasBootstrapped else { return }
        self.hasBootstrapped = true

        self.statusObservation = await self.agent.observeStatus { status in
            Task { @MainActor in
                self.status = status
            }
        }

        do {
            try await self.agent.start()
        } catch {
            // WendyAgent publishes failure state directly.
        }
    }

    @MainActor
    private func quit() {
        guard !self.isQuitting else { return }
        self.isQuitting = true

        Task {
            await self.cancelStatusObservation()
            await self.agent.stop()
            NSApplication.shared.terminate(nil)
        }
    }

    @MainActor
    private func cancelStatusObservation() async {
        guard let statusObservation = self.statusObservation else { return }
        self.statusObservation = nil
        await statusObservation.cancel()
    }
}
