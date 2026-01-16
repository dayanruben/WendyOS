import Foundation
import Logging
import OpenTelemetryGRPC

/// Actor that broadcasts telemetry data to multiple subscribers.
/// Used to fan out logs and metrics from the OTel proxy to CLI clients.
actor TelemetryBroadcaster {
    typealias LogsRequest = Opentelemetry_Proto_Collector_Logs_V1_ExportLogsServiceRequest
    typealias MetricsRequest = Opentelemetry_Proto_Collector_Metrics_V1_ExportMetricsServiceRequest

    private var logSubscribers: [UUID: AsyncStream<LogsRequest>.Continuation] = [:]
    private var metricsSubscribers: [UUID: AsyncStream<MetricsRequest>.Continuation] = [:]
    private let logger = Logger(label: "sh.wendy.agent.telemetry-broadcaster")

    init() {}

    /// Subscribe to log broadcasts. Returns a stream that yields log requests.
    func subscribeLogs() -> (id: UUID, stream: AsyncStream<LogsRequest>) {
        let id = UUID()
        let (stream, continuation) = AsyncStream<LogsRequest>.makeStream()
        logSubscribers[id] = continuation
        logger.debug("Log subscriber added", metadata: ["id": "\(id)"])
        return (id, stream)
    }

    /// Unsubscribe from log broadcasts.
    func unsubscribeLogs(id: UUID) {
        if let continuation = logSubscribers.removeValue(forKey: id) {
            continuation.finish()
            logger.debug("Log subscriber removed", metadata: ["id": "\(id)"])
        }
    }

    /// Broadcast logs to all subscribers.
    func broadcastLogs(_ request: LogsRequest) {
        for (id, continuation) in logSubscribers {
            let result = continuation.yield(request)
            if case .terminated = result {
                logSubscribers.removeValue(forKey: id)
            }
        }
    }

    /// Subscribe to metrics broadcasts. Returns a stream that yields metrics requests.
    func subscribeMetrics() -> (id: UUID, stream: AsyncStream<MetricsRequest>) {
        let id = UUID()
        let (stream, continuation) = AsyncStream<MetricsRequest>.makeStream()
        metricsSubscribers[id] = continuation
        logger.debug("Metrics subscriber added", metadata: ["id": "\(id)"])
        return (id, stream)
    }

    /// Unsubscribe from metrics broadcasts.
    func unsubscribeMetrics(id: UUID) {
        if let continuation = metricsSubscribers.removeValue(forKey: id) {
            continuation.finish()
            logger.debug("Metrics subscriber removed", metadata: ["id": "\(id)"])
        }
    }

    /// Broadcast metrics to all subscribers.
    func broadcastMetrics(_ request: MetricsRequest) {
        for (id, continuation) in metricsSubscribers {
            let result = continuation.yield(request)
            if case .terminated = result {
                metricsSubscribers.removeValue(forKey: id)
            }
        }
    }
}
