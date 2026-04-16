import Foundation
import Testing

@testable import WendyAgentCore

@MainActor
@Suite("WendyAgent", .serialized)
struct WendyAgentTests {
    @Test("startup reaches running only after all required subsystems report readiness")
    func startupReachesRunningOnlyAfterRequiredSubsystemsAreReady() async throws {
        let mainReady = AsyncGate()
        let otelReady = AsyncGate()
        let bonjourReady = AsyncGate()

        let main = ControlledComponent(name: "main")
        let otel = ControlledComponent(name: "otel")
        let bonjour = ControlledComponent(name: "bonjour")

        let agent = WendyAgent(
            testHooks: WendyAgent.TestHooks(
                prepareDockerIfNeeded: { true },
                startMainServer: { _, _ in
                    await mainReady.wait()
                    return makeMainServerRuntime(main)
                },
                startOTelServer: { _ in
                    await otelReady.wait()
                    return makeOTelServerRuntime(otel)
                },
                startBonjour: {
                    await bonjourReady.wait()
                    return makeBonjourRuntime(bonjour)
                }
            )
        )

        let startTask = Task {
            try await agent.start()
        }

        try await waitUntil { agent.status == .starting }
        #expect(agent.status == .starting)

        await mainReady.open()
        try await Task.sleep(for: .milliseconds(20))
        #expect(agent.status == .starting)

        await otelReady.open()
        try await Task.sleep(for: .milliseconds(20))
        #expect(agent.status == .starting)

        await bonjourReady.open()
        try await startTask.value

        #expect(agent.status == .running)

        await agent.stop()
        #expect(agent.status == .stopped)
    }

    @Test("Docker unavailability does not block startup")
    func dockerUnavailabilityDoesNotBlockStartup() async throws {
        let dockerAvailability = Box<Bool>()
        let shutdowns = EventLog()
        let main = ControlledComponent(name: "main", shutdowns: shutdowns)
        let otel = ControlledComponent(name: "otel", shutdowns: shutdowns)
        let bonjour = ControlledComponent(name: "bonjour", shutdowns: shutdowns)

        let agent = WendyAgent(
            testHooks: WendyAgent.TestHooks(
                prepareDockerIfNeeded: { false },
                startMainServer: { dockerAvailable, _ in
                    await dockerAvailability.set(dockerAvailable)
                    return makeMainServerRuntime(main)
                },
                startOTelServer: { _ in makeOTelServerRuntime(otel) },
                startBonjour: { makeBonjourRuntime(bonjour) }
            )
        )

        try await agent.start()

        #expect(agent.status == .running)
        #expect(await dockerAvailability.get() == false)

        await agent.stop()
        #expect(agent.status == .stopped)
    }

    @Test("required subsystem startup failure leaves WendyAgent out of running")
    func requiredSubsystemStartupFailurePreventsRunning() async throws {
        let shutdowns = EventLog()
        let main = ControlledComponent(name: "main", shutdowns: shutdowns)
        let startupError = TestAgentError("OTel startup failed")

        let agent = WendyAgent(
            testHooks: WendyAgent.TestHooks(
                prepareDockerIfNeeded: { true },
                startMainServer: { _, _ in makeMainServerRuntime(main) },
                startOTelServer: { _ in throw startupError },
                startBonjour: {
                    Issue.record("Bonjour should not start after OTel startup failure")
                    return makeBonjourRuntime(ControlledComponent(name: "bonjour"))
                }
            )
        )

        await #expect(throws: TestAgentError.self) {
            try await agent.start()
        }

        #expect(agent.status == .failed(startupError.localizedDescription))
        #expect(await shutdowns.snapshot() == ["main"])
    }

    @Test("stop transitions WendyAgent back to stopped")
    func stopTransitionsBackToStopped() async throws {
        let shutdowns = EventLog()
        let main = ControlledComponent(name: "main", shutdowns: shutdowns)
        let otel = ControlledComponent(name: "otel", shutdowns: shutdowns)
        let bonjour = ControlledComponent(name: "bonjour", shutdowns: shutdowns)

        let agent = WendyAgent(
            testHooks: WendyAgent.TestHooks(
                prepareDockerIfNeeded: { true },
                startMainServer: { _, _ in makeMainServerRuntime(main) },
                startOTelServer: { _ in makeOTelServerRuntime(otel) },
                startBonjour: { makeBonjourRuntime(bonjour) }
            )
        )

        try await agent.start()
        #expect(agent.status == .running)

        await agent.stop()

        #expect(agent.status == .stopped)
        #expect(await shutdowns.snapshot() == ["main", "otel", "bonjour"])
    }

    @Test("unexpected runtime exit transitions WendyAgent away from running")
    func unexpectedRuntimeExitTransitionsAwayFromRunning() async throws {
        let shutdowns = EventLog()
        let main = ControlledComponent(name: "main", shutdowns: shutdowns)
        let otel = ControlledComponent(name: "otel", shutdowns: shutdowns)
        let bonjour = ControlledComponent(name: "bonjour", shutdowns: shutdowns)
        let runtimeError = TestAgentError("OTel runtime failed")

        let agent = WendyAgent(
            testHooks: WendyAgent.TestHooks(
                prepareDockerIfNeeded: { true },
                startMainServer: { _, _ in makeMainServerRuntime(main) },
                startOTelServer: { _ in makeOTelServerRuntime(otel) },
                startBonjour: { makeBonjourRuntime(bonjour) }
            )
        )

        try await agent.start()
        #expect(agent.status == .running)

        await otel.taskController.fail(runtimeError)

        try await waitUntil {
            agent.status != .running
        }

        #expect(agent.status == .failed(runtimeError.localizedDescription))
        #expect(await shutdowns.snapshot() == ["main", "otel", "bonjour"])
    }
}

private struct ControlledComponent {
    let name: String
    let taskController: TaskController
    let shutdowns: EventLog

    init(
        name: String,
        taskController: TaskController = TaskController(),
        shutdowns: EventLog = EventLog()
    ) {
        self.name = name
        self.taskController = taskController
        self.shutdowns = shutdowns
    }
}

private actor AsyncGate {
    private var isOpen = false
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func wait() async {
        if self.isOpen {
            return
        }

        await withCheckedContinuation { continuation in
            self.waiters.append(continuation)
        }
    }

    func open() {
        guard !self.isOpen else { return }
        self.isOpen = true
        let waiters = self.waiters
        self.waiters.removeAll()
        for waiter in waiters {
            waiter.resume()
        }
    }
}

private actor TaskController {
    private var result: Result<Void, Error>?
    private var waiters: [CheckedContinuation<Void, Error>] = []

    func wait() async throws {
        if let result = self.result {
            try result.get()
            return
        }

        try await withCheckedThrowingContinuation { continuation in
            self.waiters.append(continuation)
        }
    }

    func finish() {
        self.resolve(.success(()))
    }

    func fail(_ error: some Error) {
        self.resolve(.failure(error))
    }

    private func resolve(_ result: Result<Void, Error>) {
        guard self.result == nil else { return }
        self.result = result

        let waiters = self.waiters
        self.waiters.removeAll()
        for waiter in waiters {
            switch result {
            case .success:
                waiter.resume(returning: ())
            case .failure(let error):
                waiter.resume(throwing: error)
            }
        }
    }
}

private actor EventLog {
    private var events: [String] = []

    func record(_ event: String) {
        self.events.append(event)
    }

    func snapshot() -> [String] {
        self.events
    }
}

private actor Box<Value: Sendable> {
    private var value: Value

    init(_ value: Value) {
        self.value = value
    }

    init() where Value == Bool {
        self.value = false
    }

    func get() -> Value {
        self.value
    }

    func set(_ value: Value) {
        self.value = value
    }
}

private struct TestAgentError: Error, LocalizedError {
    let message: String

    init(_ message: String) {
        self.message = message
    }

    var errorDescription: String? {
        self.message
    }
}

@MainActor
private func waitUntil(
    timeout: Duration = .seconds(1),
    pollInterval: Duration = .milliseconds(10),
    condition: @escaping @MainActor () -> Bool
) async throws {
    let clock = ContinuousClock()
    let deadline = clock.now.advanced(by: timeout)

    while clock.now < deadline {
        if condition() {
            return
        }

        try await Task.sleep(for: pollInterval)
    }

    throw TestAgentError("Timed out waiting for condition")
}

private func makeMainServerRuntime(_ component: ControlledComponent) -> WendyAgent.MainServerRuntime {
    let taskController = component.taskController
    let shutdowns = component.shutdowns
    let name = component.name

    return WendyAgent.MainServerRuntime(
        task: Task {
            try await taskController.wait()
        },
        shutdown: {
            await shutdowns.record(name)
            await taskController.finish()
        }
    )
}

private func makeOTelServerRuntime(_ component: ControlledComponent) -> WendyAgent.OTelServerRuntime {
    let taskController = component.taskController
    let shutdowns = component.shutdowns
    let name = component.name

    return WendyAgent.OTelServerRuntime(
        task: Task {
            try await taskController.wait()
        },
        shutdown: {
            await shutdowns.record(name)
            await taskController.finish()
        }
    )
}

private func makeBonjourRuntime(_ component: ControlledComponent) -> WendyAgent.BonjourRuntime {
    let taskController = component.taskController
    let shutdowns = component.shutdowns
    let name = component.name

    return WendyAgent.BonjourRuntime(
        task: Task {
            try await taskController.wait()
        },
        shutdown: {
            await shutdowns.record(name)
            await taskController.finish()
        }
    )
}
