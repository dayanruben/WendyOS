import GRPCCore

enum UnsupportedRPC {
    static let message = "Not supported on macOS yet."

    private static let transportCode: RPCError.Code = .unimplemented

    static func error() -> RPCError {
        RPCError(code: self.transportCode, message: self.message)
    }

    static func isUnsupported(_ error: any Error) -> Bool {
        guard let rpcError = error as? RPCError else { return false }
        return rpcError.code == self.transportCode && rpcError.message == self.message
    }
}
