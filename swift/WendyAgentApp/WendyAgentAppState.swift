import AppKit
import Combine
import WendyAgent

@MainActor
final class WendyAgentAppState: ObservableObject {
    // MARK: - Internal

    @Published private(set) var status: WendyAgentStatus = .idle

    init(agent: WendyAgent = WendyAgent()) {
        self.agent = agent
        self.statusObservationTask = Task { [weak self] in
            let observation = await agent.observeStatus { [weak self] status in
                Task { @MainActor [weak self] in
                    self?.status = status
                }
            }

            await MainActor.run {
                self?.statusObservation = observation
            }
        }
    }

    deinit {
        self.statusObservationTask?.cancel()
        if let statusObservation {
            Task {
                await statusObservation.cancel()
            }
        }
    }

    func startIfNeeded() {
        guard self.startupTask == nil else { return }

        self.startupTask = Task {
            do {
                try await self.agent.start()
            } catch {
                // WendyAgent publishes failure state directly.
            }
        }
    }

    func quit() {
        guard self.quitTask == nil else { return }

        self.quitTask = Task {
            if let statusObservation = self.statusObservation {
                await statusObservation.cancel()
                self.statusObservation = nil
            }
            await self.agent.stop()
            NSApplication.shared.terminate(nil)
        }
    }

    // MARK: - Private

    private let agent: WendyAgent
    private var statusObservation: WendyObservation?
    private var statusObservationTask: Task<Void, Never>?
    private var startupTask: Task<Void, Never>?
    private var quitTask: Task<Void, Never>?
}
