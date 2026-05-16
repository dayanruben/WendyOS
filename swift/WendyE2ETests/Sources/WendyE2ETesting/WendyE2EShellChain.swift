public struct WendyE2EShellChain: Sendable {
    public enum PollCondition: Sendable {
        case success
        case failure
    }

    public let command: String

    public func poll(
        until condition: PollCondition,
        step: Duration = .milliseconds(250),
        timeout: Duration = .seconds(10),
        timeoutMessage: String? = nil
    ) -> WendyE2EShellChain {
        precondition(step > .zero, "step must be greater than zero")
        precondition(timeout >= .zero, "timeout must be greater than or equal to zero")

        return WendyE2EShellChain(
            session: self.session,
            command: self.command,
            pollConfiguration: PollConfiguration(
                condition: condition,
                step: step,
                timeout: timeout,
                timeoutMessage: timeoutMessage
            )
        )
    }

    @discardableResult
    public func run() async throws -> WendyE2EShellResult {
        let result = try await self.result()
        if self.pollConfiguration == nil {
            try result.requireSuccess()
        }
        return result
    }

    public func run<Result>(
        body: @Sendable (_ result: WendyE2EShellResult) async throws -> Result
    ) async throws -> Result {
        try await body(try await self.result())
    }

    // MARK: - Internal

    init(session: WendyE2ESession, command: String) {
        self.session = session
        self.command = command
        self.pollConfiguration = nil
    }

    // MARK: - Private

    private let session: WendyE2ESession
    private let pollConfiguration: PollConfiguration?

    private init(
        session: WendyE2ESession,
        command: String,
        pollConfiguration: PollConfiguration?
    ) {
        self.session = session
        self.command = command
        self.pollConfiguration = pollConfiguration
    }

    private func result() async throws -> WendyE2EShellResult {
        guard let pollConfiguration else {
            return try await self.runOnce()
        }

        return try await self.poll(pollConfiguration)
    }

    private func poll(_ configuration: PollConfiguration) async throws -> WendyE2EShellResult {
        let clock = ContinuousClock()
        let start = clock.now
        var lastStatus: WendyE2EShellStatus?

        while true {
            let result = try await self.runOnce()
            lastStatus = result.status

            if configuration.condition.matches(result.status) {
                return result
            }

            let elapsed = start.duration(to: clock.now)
            guard elapsed < configuration.timeout else {
                throw WendyE2EMachineError.pollTimedOut(
                    machine: self.session.description,
                    command: self.command,
                    condition: configuration.condition.description,
                    timeout: configuration.timeout,
                    lastStatus: lastStatus,
                    message: configuration.timeoutMessage
                )
            }

            try await clock.sleep(for: min(configuration.step, configuration.timeout - elapsed))
        }
    }

    private func runOnce() async throws -> WendyE2EShellResult {
        switch self.session.machine.os {
        case .windows:
            try await self.session.powerShell(self.command)
        case .macOS, .linux, .wendyOS:
            try await self.session.posixShell(self.command)
        }
    }
}

// MARK: - CustomStringConvertible

extension WendyE2EShellChain.PollCondition: CustomStringConvertible {
    public var description: String {
        switch self {
        case .success:
            return "success"
        case .failure:
            return "failure"
        }
    }
}

// MARK: - Private

private struct PollConfiguration: Sendable {
    let condition: WendyE2EShellChain.PollCondition
    let step: Duration
    let timeout: Duration
    let timeoutMessage: String?
}

extension WendyE2EShellChain.PollCondition {
    fileprivate func matches(_ status: WendyE2EShellStatus) -> Bool {
        switch self {
        case .success:
            status.isSuccess
        case .failure:
            status.isFailure
        }
    }
}
