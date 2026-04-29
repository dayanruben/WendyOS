import Subprocess

public enum MachineError: Error {
    case invalidMachineSpec(String)
    case commandFailed(machine: String, command: String, terminationStatus: TerminationStatus)
}

// MARK: - CustomStringConvertible

extension MachineError: CustomStringConvertible {
    public var description: String {
        switch self {
        case .invalidMachineSpec(let spec):
            return "Invalid machine spec: \(spec)"
        case .commandFailed(let machine, let command, let terminationStatus):
            return "Command failed on \(machine) with \(terminationStatus): \(command)"
        }
    }
}
