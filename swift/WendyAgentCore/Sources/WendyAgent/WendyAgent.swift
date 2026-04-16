import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2
import Logging
import OpenTelemetryGRPC
import WendyAgentGRPC

@MainActor
public final class WendyAgent {
    private typealias PosixGRPCServer = GRPCServer<HTTP2ServerTransport.Posix>

    struct MainServerRuntime {
        let task: Task<Void, Error>
        let shutdown: () async -> Void
    }

    struct OTelServerRuntime {
        let task: Task<Void, Error>
        let shutdown: () async -> Void
    }

    struct BonjourRuntime {
        let task: Task<Void, Error>
        let shutdown: () async -> Void
    }

    struct TestHooks {
        var prepareDockerIfNeeded: (() async -> Bool)?
        var startMainServer: ((Bool, TelemetryBroadcaster) async throws -> MainServerRuntime)?
        var startOTelServer: ((TelemetryBroadcaster) async throws -> OTelServerRuntime)?
        var startBonjour: (() async throws -> BonjourRuntime)?
    }

    public let configuration: WendyAgentConfiguration
    public private(set) var status: WendyAgentStatus = .idle

    public init(configuration: WendyAgentConfiguration = .init()) {
        self.configuration = configuration
        self.testHooks = nil
    }

    init(
        configuration: WendyAgentConfiguration = .init(),
        testHooks: TestHooks
    ) {
        self.configuration = configuration
        self.testHooks = testHooks
    }

    public func start() async throws {
        switch self.status {
        case .idle, .stopped, .failed:
            break
        case .starting, .running, .stopping:
            return
        }

        Self.bootstrapLogging
        self.updateStatus(.starting)

        self.logger.info(
            "Starting Wendy Agent",
            metadata: [
                "grpc_port": "\(self.configuration.port)",
                "otel_port": "\(self.configuration.otelPort)",
            ]
        )

        let broadcaster = TelemetryBroadcaster()
        var mainServerRuntime: MainServerRuntime?
        var otelServerRuntime: OTelServerRuntime?
        var bonjourRuntime: BonjourRuntime?

        do {
            let dockerAvailable = await self.prepareDockerIfNeeded()

            let startedMainServerRuntime = try await self.startMainServer(
                dockerAvailable: dockerAvailable,
                broadcaster: broadcaster
            )
            mainServerRuntime = startedMainServerRuntime

            let startedOTelServerRuntime = try await self.startOTelServer(broadcaster: broadcaster)
            otelServerRuntime = startedOTelServerRuntime

            let startedBonjourRuntime = try await self.startBonjour()
            bonjourRuntime = startedBonjourRuntime

            self.runIdentifier &+= 1
            let runIdentifier = self.runIdentifier
            self.mainServerRuntime = startedMainServerRuntime
            self.otelServerRuntime = startedOTelServerRuntime
            self.bonjourRuntime = startedBonjourRuntime
            self.startMonitorTask(
                mainServerRuntime: startedMainServerRuntime,
                otelServerRuntime: startedOTelServerRuntime,
                bonjourRuntime: startedBonjourRuntime,
                runIdentifier: runIdentifier
            )

            self.logger.info(
                "Wendy Agent is running",
                metadata: [
                    "grpc_port": "\(self.configuration.port)",
                    "otel_port": "\(self.configuration.otelPort)",
                ]
            )
            self.updateStatus(.running)
        } catch {
            await self.shutdownRuntime(
                mainServerRuntime: mainServerRuntime,
                otelServerRuntime: otelServerRuntime,
                bonjourRuntime: bonjourRuntime
            )
            self.clearRuntimeState()
            self.updateStatus(.failed(Self.errorMessage(for: error)))
            throw error
        }
    }

    public func stop() async {
        guard case .running = self.status else {
            return
        }

        guard let mainServerRuntime = self.mainServerRuntime,
              let otelServerRuntime = self.otelServerRuntime,
              let bonjourRuntime = self.bonjourRuntime
        else {
            self.clearRuntimeState()
            self.updateStatus(.stopped)
            return
        }

        self.logger.info("Stopping Wendy Agent")
        self.updateStatus(.stopping)
        self.monitorTask?.cancel()
        self.monitorTask = nil

        await self.shutdownRuntime(
            mainServerRuntime: mainServerRuntime,
            otelServerRuntime: otelServerRuntime,
            bonjourRuntime: bonjourRuntime
        )

        self.clearRuntimeState()
        self.updateStatus(.stopped)
        self.logger.info("Wendy Agent stopped")
    }

    public func observeStatus(
        _ handler: @escaping @isolated(any) @Sendable (WendyAgentStatus) -> Void
    ) -> WendyObservation {
        let observationID = self.statusObservationRegistry.register(handler, initialValue: self.status)
        self.scheduleStatusObservation(for: observationID)

        return WendyObservation { [self] in
            await self.cancelStatusObservation(for: observationID)
        }
    }

    // MARK: - Private

    private static let bootstrapLogging: Void = {
        LoggingSystem.bootstrap { label in
            var handler = StreamLogHandler.standardError(label: label)
            handler.logLevel = .info
            return handler
        }
    }()

    private let logger = Logger(label: "sh.wendy.agent")
    private let testHooks: TestHooks?

    private var mainServerRuntime: MainServerRuntime?
    private var otelServerRuntime: OTelServerRuntime?
    private var bonjourRuntime: BonjourRuntime?
    private var monitorTask: Task<Void, Never>?
    private var runIdentifier: UInt64 = 0
    private var handlingUnexpectedRuntimeExit = false
    private var statusObservationRegistry = WendyObservationRegistry<WendyAgentStatus>(areEquivalent: ==)
    private var statusObservationTasks: [WendyObservationRegistry<WendyAgentStatus>.ObservationID: Task<Void, Never>] = [:]

    private func prepareDockerIfNeeded() async -> Bool {
        if let prepareDockerIfNeeded = self.testHooks?.prepareDockerIfNeeded {
            return await prepareDockerIfNeeded()
        }

        let docker = DockerCLI()
        let dockerAvailable = await docker.checkAvailable()
        if dockerAvailable {
            do {
                try await docker.ensureRegistry()
            } catch {
                self.logger.warning(
                    "Failed to start Docker registry: \(String(describing: error)). Linux container support disabled."
                )
            }
        } else {
            self.logger.info("Docker not available, Linux container support disabled")
        }

        return dockerAvailable
    }

    private func startMainServer(
        dockerAvailable: Bool,
        broadcaster: TelemetryBroadcaster
    ) async throws -> MainServerRuntime {
        if let startMainServer = self.testHooks?.startMainServer {
            return try await startMainServer(dockerAvailable, broadcaster)
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

        let server = PosixGRPCServer(
            transport: HTTP2ServerTransport.Posix(
                address: .ipv6(host: "::", port: self.configuration.port),
                transportSecurity: .plaintext
            ),
            services: services
        )

        let task = Self.makeServeTask(server: server)

        do {
            if let address = try await server.listeningAddress {
                self.logger.info(
                    "Main Wendy Agent gRPC server listening",
                    metadata: ["grpc_address": "\(address)"]
                )
            } else {
                self.logger.info("Main Wendy Agent gRPC server listening")
            }

            return MainServerRuntime(
                task: task,
                shutdown: {
                    server.beginGracefulShutdown()
                }
            )
        } catch {
            server.beginGracefulShutdown()
            _ = try? await task.value
            throw error
        }
    }

    private func startOTelServer(
        broadcaster: TelemetryBroadcaster
    ) async throws -> OTelServerRuntime {
        if let startOTelServer = self.testHooks?.startOTelServer {
            return try await startOTelServer(broadcaster)
        }

        let services: [any RegistrableRPCService] = [
            LocalOTelLogsReceiver(broadcaster: broadcaster),
            LocalOTelMetricsReceiver(broadcaster: broadcaster),
            LocalOTelTracesReceiver(broadcaster: broadcaster),
        ]

        let server = PosixGRPCServer(
            transport: HTTP2ServerTransport.Posix(
                address: .ipv4(host: "127.0.0.1", port: self.configuration.otelPort),
                transportSecurity: .plaintext
            ),
            services: services
        )

        let task = Self.makeServeTask(server: server)

        do {
            if let address = try await server.listeningAddress {
                self.logger.info(
                    "Local OpenTelemetry gRPC server listening",
                    metadata: ["otel_address": "\(address)"]
                )
            } else {
                self.logger.info("Local OpenTelemetry gRPC server listening")
            }

            return OTelServerRuntime(
                task: task,
                shutdown: {
                    server.beginGracefulShutdown()
                }
            )
        } catch {
            server.beginGracefulShutdown()
            _ = try? await task.value
            throw error
        }
    }

    private func startBonjour() async throws -> BonjourRuntime {
        if let startBonjour = self.testHooks?.startBonjour {
            return try await startBonjour()
        }

        let advertiser = BonjourAdvertiser(
            port: self.configuration.port,
            displayName: ProcessInfo.processInfo.hostName,
            deviceID: ProcessInfo.processInfo.hostName
        )

        let runtime = try await advertiser.start()
        self.logger.info("Bonjour advertisement registered")

        return BonjourRuntime(
            task: runtime.task,
            shutdown: {
                await runtime.registration.shutdown()
            }
        )
    }

    private func startMonitorTask(
        mainServerRuntime: MainServerRuntime,
        otelServerRuntime: OTelServerRuntime,
        bonjourRuntime: BonjourRuntime,
        runIdentifier: UInt64
    ) {
        self.monitorTask?.cancel()
        self.monitorTask = Self.makeMonitorTask(
            agent: self,
            mainServerTask: mainServerRuntime.task,
            otelServerTask: otelServerRuntime.task,
            bonjourTask: bonjourRuntime.task,
            runIdentifier: runIdentifier
        )
    }

    private func shutdownRuntime(
        mainServerRuntime: MainServerRuntime?,
        otelServerRuntime: OTelServerRuntime?,
        bonjourRuntime: BonjourRuntime?
    ) async {
        await mainServerRuntime?.shutdown()
        await otelServerRuntime?.shutdown()
        await bonjourRuntime?.shutdown()

        _ = try? await mainServerRuntime?.task.value
        _ = try? await otelServerRuntime?.task.value
        _ = try? await bonjourRuntime?.task.value
    }

    private func clearRuntimeState() {
        self.mainServerRuntime = nil
        self.otelServerRuntime = nil
        self.bonjourRuntime = nil
        self.monitorTask?.cancel()
        self.monitorTask = nil
        self.handlingUnexpectedRuntimeExit = false
    }

    private func monitorRuntimeTasks(
        mainServerTask: Task<Void, Error>,
        otelServerTask: Task<Void, Error>,
        bonjourTask: Task<Void, Error>,
        runIdentifier: UInt64
    ) async {
        await withTaskGroup(of: Void.self) { group in
            group.addTask {
                await self.monitorRuntimeTask(
                    mainServerTask,
                    subsystem: "main_grpc",
                    runIdentifier: runIdentifier
                )
            }
            group.addTask {
                await self.monitorRuntimeTask(
                    otelServerTask,
                    subsystem: "otel_grpc",
                    runIdentifier: runIdentifier
                )
            }
            group.addTask {
                await self.monitorRuntimeTask(
                    bonjourTask,
                    subsystem: "bonjour",
                    runIdentifier: runIdentifier
                )
            }

            await group.next()
            group.cancelAll()
        }
    }

    private func monitorRuntimeTask(
        _ task: Task<Void, Error>,
        subsystem: String,
        runIdentifier: UInt64
    ) async {
        do {
            try await task.value
            guard !Task.isCancelled else { return }
            await self.handleUnexpectedRuntimeExit(
                subsystem: subsystem,
                error: nil,
                runIdentifier: runIdentifier
            )
        } catch is CancellationError {
            return
        } catch {
            await self.handleUnexpectedRuntimeExit(
                subsystem: subsystem,
                error: error,
                runIdentifier: runIdentifier
            )
        }
    }

    private func handleUnexpectedRuntimeExit(
        subsystem: String,
        error: (any Error)?,
        runIdentifier: UInt64
    ) async {
        guard !Task.isCancelled,
              !self.handlingUnexpectedRuntimeExit,
              self.runIdentifier == runIdentifier,
              case .running = self.status,
              let mainServerRuntime = self.mainServerRuntime,
              let otelServerRuntime = self.otelServerRuntime,
              let bonjourRuntime = self.bonjourRuntime
        else {
            return
        }

        self.handlingUnexpectedRuntimeExit = true

        if let error {
            self.logger.error(
                "Runtime subsystem stopped unexpectedly",
                metadata: [
                    "subsystem": "\(subsystem)",
                    "error": "\(Self.errorMessage(for: error))",
                ]
            )
        } else {
            self.logger.warning(
                "Runtime subsystem stopped unexpectedly",
                metadata: ["subsystem": "\(subsystem)"]
            )
        }

        await self.shutdownRuntime(
            mainServerRuntime: mainServerRuntime,
            otelServerRuntime: otelServerRuntime,
            bonjourRuntime: bonjourRuntime
        )

        self.clearRuntimeState()

        if let error {
            self.updateStatus(.failed(Self.errorMessage(for: error)))
        } else {
            self.updateStatus(.stopped)
        }
    }

    private func updateStatus(_ status: WendyAgentStatus) {
        self.status = status

        let observationIDs = self.statusObservationRegistry.enqueue(status)
        for observationID in observationIDs {
            self.scheduleStatusObservation(for: observationID)
        }
    }

    private func scheduleStatusObservation(
        for observationID: WendyObservationRegistry<WendyAgentStatus>.ObservationID
    ) {
        guard self.statusObservationTasks[observationID] == nil else { return }

        let task = Task { @MainActor in
            await self.runStatusObservation(for: observationID)
        }
        self.statusObservationTasks[observationID] = task
    }

    private func runStatusObservation(
        for observationID: WendyObservationRegistry<WendyAgentStatus>.ObservationID
    ) async {
        while let delivery = self.statusObservationRegistry.beginDelivery(for: observationID) {
            await delivery.handler(delivery.value)

            let shouldContinue = self.statusObservationRegistry.finishDelivery(
                for: observationID,
                delivered: delivery.value
            )
            guard shouldContinue else { break }
        }

        self.statusObservationTasks.removeValue(forKey: observationID)
    }

    private func cancelStatusObservation(
        for observationID: WendyObservationRegistry<WendyAgentStatus>.ObservationID
    ) async {
        self.statusObservationRegistry.removeObservation(observationID)
        let task = self.statusObservationTasks.removeValue(forKey: observationID)
        await task?.value
    }

    nonisolated private static func makeMonitorTask(
        agent: WendyAgent,
        mainServerTask: Task<Void, Error>,
        otelServerTask: Task<Void, Error>,
        bonjourTask: Task<Void, Error>,
        runIdentifier: UInt64
    ) -> Task<Void, Never> {
        Task.detached {
            await agent.monitorRuntimeTasks(
                mainServerTask: mainServerTask,
                otelServerTask: otelServerTask,
                bonjourTask: bonjourTask,
                runIdentifier: runIdentifier
            )
        }
    }

    nonisolated private static func makeServeTask(server: PosixGRPCServer) -> Task<Void, Error> {
        Task {
            try await server.serve()
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
