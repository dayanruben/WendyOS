public enum WendyE2EMachineError: Error {
    case commandFailed(machine: String, command: String, status: WendyE2EShellStatus)
    case powerShellUnavailable(machine: String)
    case pollTimedOut(
        machine: String,
        command: String,
        condition: String,
        timeout: Duration,
        lastStatus: WendyE2EShellStatus?,
        message: String?
    )
}

// MARK: - CustomStringConvertible

extension WendyE2EMachineError: CustomStringConvertible {
    public var description: String {
        switch self {
        case .commandFailed(let machine, let command, let status):
            return "Command failed on \(machine) with \(status): \(command)"
        case .powerShellUnavailable(let machine):
            return "PowerShell is not available on \(machine)"
        case .pollTimedOut(
            let machine,
            let command,
            let condition,
            let timeout,
            let lastStatus,
            let message
        ):
            let prefix = message.map { "\($0): " } ?? ""
            let lastStatusDescription = lastStatus.map(String.init(describing:)) ?? "<none>"
            return "\(prefix)Command on \(machine) did not reach \(condition) within \(timeout)"
                + " (last status: \(lastStatusDescription)): \(command)"
        }
    }
}
