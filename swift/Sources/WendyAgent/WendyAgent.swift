import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2
import GRPCServiceLifecycle
import Logging
import OpenTelemetryGRPC
import ServiceLifecycle
import WendyAgentGRPC

public actor WendyAgent {
    public let configuration: WendyAgentConfiguration
    public private(set) var status: WendyAgentStatus = .idle

    public init(configuration: WendyAgentConfiguration = .init()) {
        self.configuration = configuration
    }

    public func start() async throws {
        guard self.runTask == nil else { return }

        Self.bootstrapLogging
        self.updateStatus(.starting)

        let logger = Logger(label: "sh.wendy.agent")
        logger.info("Starting Wendy Agent on port \(self.configuration.port)")

        let broadcaster = TelemetryBroadcaster()

        let docker = DockerCLI()
        let dockerAvailable = await docker.checkAvailable()
        if dockerAvailable {
            logger.info(
                "Docker detected, starting local registry on port \(DockerCLI.registryPort) for Linux container support"
            )
            do {
                try await docker.ensureRegistry()
            } catch {
                logger.warning(
                    "Failed to start Docker registry: \(String(describing: error)). Linux container support disabled."
                )
            }
        } else {
            logger.info("Docker not found, Linux container support disabled")
        }

        let appsBase = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent("Library/Application Support/wendy-agent/apps")

        let services: [any RegistrableRPCService] = [
            AgentService(),
            ContainerService(
                broadcaster: broadcaster,
                executablePath: self.configuration.appPath,
                sandboxProfilePath: self.configuration.sandboxProfile.isEmpty
                    ? nil
                    : self.configuration.sandboxProfile,
                appsBase: appsBase,
                dockerAvailable: dockerAvailable
            ),
            AudioService(),
            ProvisioningService(),
            TelemetryService(broadcaster: broadcaster),
            FileSyncService(appsBase: appsBase),
        ]

        let server = GRPCServer(
            transport: HTTP2ServerTransport.Posix(
                address: .ipv6(host: "::", port: self.configuration.port),
                transportSecurity: .plaintext
            ),
            services: services
        )

        let otelServices: [any RegistrableRPCService] = [
            LocalOTelLogsReceiver(broadcaster: broadcaster),
            LocalOTelMetricsReceiver(broadcaster: broadcaster),
            LocalOTelTracesReceiver(broadcaster: broadcaster),
        ]

        let otelServer = GRPCServer(
            transport: HTTP2ServerTransport.Posix(
                address: .ipv4(host: "127.0.0.1", port: self.configuration.otelPort),
                transportSecurity: .plaintext
            ),
            services: otelServices
        )

        let bonjour = BonjourAdvertiser(
            port: self.configuration.port,
            displayName: ProcessInfo.processInfo.hostName,
            deviceID: ProcessInfo.processInfo.hostName
        )

        let serviceGroup = ServiceGroup(
            configuration: .init(
                services: [
                    .init(service: server),
                    .init(service: otelServer),
                    .init(service: bonjour),
                ],
                logger: logger
            )
        )

        let runTask = Task {
            try await serviceGroup.run()
        }

        self.runIdentifier &+= 1
        let runIdentifier = self.runIdentifier
        self.serviceGroup = serviceGroup
        self.runTask = runTask
        self.runTaskMonitor?.cancel()
        self.runTaskMonitor = Task {
            await self.monitorRunTask(runTask, runIdentifier: runIdentifier)
        }

        logger.info(
            "Listening on port \(self.configuration.port), OTel on port \(self.configuration.otelPort)"
        )

        do {
            try await Self.waitForStartup(of: runTask)
            guard self.runIdentifier == runIdentifier, self.runTask != nil else { return }
            self.updateStatus(.running)
        } catch {
            self.finalizeRunTaskIfNeeded(runIdentifier: runIdentifier, error: error)
            throw error
        }
    }

    public func stop() async {
        guard let serviceGroup, let runTask else {
            return
        }

        self.updateStatus(.stopping)

        await serviceGroup.triggerGracefulShutdown()

        do {
            try await runTask.value
            self.finalizeRunTaskIfNeeded(runIdentifier: self.runIdentifier, error: nil)
        } catch {
            self.finalizeRunTaskIfNeeded(runIdentifier: self.runIdentifier, error: error)
        }
    }

    public func observeStatus(
        _ observer: @escaping @Sendable (WendyAgentStatus) -> Void
    ) -> WendyObservation {
        let observerID = self.statusObservationRegistry.register(observer, initialValue: self.status)
        self.scheduleStatusObservation(for: observerID)

        return WendyObservation { [self] in
            await self.cancelStatusObservation(for: observerID)
        }
    }

    // MARK: - Private

    private static let startupProbeDelay: Duration = .milliseconds(300)
    private static let bootstrapLogging: Void = {
        LoggingSystem.bootstrap { label in
            var handler = StreamLogHandler.standardError(label: label)
            handler.logLevel = .info
            return handler
        }
    }()

    private var serviceGroup: ServiceGroup?
    private var runTask: Task<Void, Error>?
    private var runTaskMonitor: Task<Void, Never>?
    private var runIdentifier: UInt64 = 0
    private var statusObservationRegistry = WendyObservationRegistry<WendyAgentStatus>(areEquivalent: ==)
    private var statusObservationTasks: [WendyObservationRegistry<WendyAgentStatus>.ObserverID: Task<Void, Never>] = [:]

    private func monitorRunTask(_ runTask: Task<Void, Error>, runIdentifier: UInt64) async {
        do {
            try await runTask.value
            self.finalizeRunTaskIfNeeded(runIdentifier: runIdentifier, error: nil)
        } catch {
            self.finalizeRunTaskIfNeeded(runIdentifier: runIdentifier, error: error)
        }
    }

    private func finalizeRunTaskIfNeeded(runIdentifier: UInt64, error: (any Error)?) {
        guard self.runIdentifier == runIdentifier, self.runTask != nil else { return }

        self.serviceGroup = nil
        self.runTask = nil
        self.runTaskMonitor?.cancel()
        self.runTaskMonitor = nil

        switch self.status {
        case .stopping:
            self.updateStatus(.stopped)
        case .starting, .running:
            if let error {
                self.updateStatus(.failed(Self.errorMessage(for: error)))
            } else {
                self.updateStatus(.stopped)
            }
        case .idle, .stopped, .failed:
            break
        }
    }

    private func updateStatus(_ status: WendyAgentStatus) {
        self.status = status

        let observerIDs = self.statusObservationRegistry.enqueue(status)
        for observerID in observerIDs {
            self.scheduleStatusObservation(for: observerID)
        }
    }

    private func scheduleStatusObservation(
        for observerID: WendyObservationRegistry<WendyAgentStatus>.ObserverID
    ) {
        guard self.statusObservationTasks[observerID] == nil else { return }

        let task = Task {
            await self.runStatusObservation(for: observerID)
        }
        self.statusObservationTasks[observerID] = task
    }

    private nonisolated func runStatusObservation(
        for observerID: WendyObservationRegistry<WendyAgentStatus>.ObserverID
    ) async {
        while let delivery = await self.beginStatusObservationDelivery(for: observerID) {
            delivery.observer(delivery.value)

            let shouldContinue = await self.finishStatusObservationDelivery(
                for: observerID,
                delivered: delivery.value
            )
            guard shouldContinue else { break }
        }

        await self.clearStatusObservationTask(for: observerID)
    }

    private func beginStatusObservationDelivery(
        for observerID: WendyObservationRegistry<WendyAgentStatus>.ObserverID
    ) -> WendyObservationRegistry<WendyAgentStatus>.Delivery? {
        self.statusObservationRegistry.beginDelivery(for: observerID)
    }

    private func finishStatusObservationDelivery(
        for observerID: WendyObservationRegistry<WendyAgentStatus>.ObserverID,
        delivered status: WendyAgentStatus
    ) -> Bool {
        self.statusObservationRegistry.finishDelivery(for: observerID, delivered: status)
    }

    private func clearStatusObservationTask(
        for observerID: WendyObservationRegistry<WendyAgentStatus>.ObserverID
    ) {
        self.statusObservationTasks.removeValue(forKey: observerID)
    }

    private func cancelStatusObservation(
        for observerID: WendyObservationRegistry<WendyAgentStatus>.ObserverID
    ) async {
        self.statusObservationRegistry.removeObserver(observerID)
        let task = self.statusObservationTasks.removeValue(forKey: observerID)
        await task?.value
    }

    private static func waitForStartup(of runTask: Task<Void, Error>) async throws {
        try await withThrowingTaskGroup(of: Void.self) { group in
            group.addTask {
                try await runTask.value
                throw WendyAgentError.stoppedDuringStartup
            }
            group.addTask {
                try await Task.sleep(for: Self.startupProbeDelay)
            }

            _ = try await group.next()
            group.cancelAll()
        }
    }

    private static func errorMessage(for error: any Error) -> String {
        if let localizedError = error as? LocalizedError,
           let description = localizedError.errorDescription,
           !description.isEmpty
        {
            return description
        }

        let description = String(describing: error)
        return description.isEmpty ? "WendyAgent failed to start." : description
    }
}
