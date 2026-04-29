import Foundation

public struct CommandOutput: Sendable, CustomStringConvertible, Equatable {
    public let string: String

    public init(_ string: String) {
        self.string = string
    }

    public var description: String {
        self.string
    }

    public func contains(_ pattern: String) -> Bool {
        self.string.range(of: pattern, options: String.CompareOptions.regularExpression) != nil
    }
}
