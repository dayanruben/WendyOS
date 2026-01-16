import ArgumentParser
import Foundation
import GRPCCore
import GRPCNIOTransportHTTP2
import Logging
import Noora
import OpenTelemetryGRPC
import WendyAgentGRPC

struct LogsCommand: AsyncParsableCommand {
    static let configuration = CommandConfiguration(
        commandName: "logs",
        abstract: "Stream logs from your device."
    )

    @Option(name: .shortAndLong, help: "Filter logs by service name")
    var service: String?

    @Option(name: .long, help: "Filter logs by app/container name")
    var app: String?

    @Option(name: .shortAndLong, help: "Minimum severity level (trace, debug, info, warn, error, fatal)")
    var level: String?

    @Flag(name: [.customShort("j"), .long], help: "Output in JSON format")
    var json: Bool = false

    @Option(name: .long, help: "Forward logs to a local OTLP collector at this address (e.g., localhost:4317)")
    var forward: String?

    @OptionGroup var agentConnectionOptions: AgentConnectionOptions

    func run() async throws {
        let minSeverity: Int32? = level.flatMap { parseSeverityLevel($0) }
        let endpoint = try await agentConnectionOptions.read(
            title: "For which device do you want to stream logs?"
        )

        if !json {
            Noora().info("Streaming logs from device... (Press Ctrl+C to stop)")
        }

        // Reconnection loop - keeps trying to connect when agent restarts
        while !Task.isCancelled {
            do {
                try await withAgentGRPCClient(endpoint, title: "") { client in
                    let telemetry = Wendy_Agent_Services_V1_WendyTelemetryService.Client(wrapping: client)

                    let request = Wendy_Agent_Services_V1_StreamLogsRequest.with {
                        if let service = service {
                            $0.serviceName = service
                        }
                        if let app = app {
                            $0.appName = app
                        }
                        if let minSeverity = minSeverity {
                            $0.minSeverity = minSeverity
                        }
                    }

                    let shouldForward = forward != nil

                    try await telemetry.streamLogs(request) { response in
                        switch response.accepted {
                        case .success(let contents):
                            for try await bodyPart in contents.bodyParts {
                                switch bodyPart {
                                case .message(let message):
                                    if shouldForward {
                                        // Forward to local collector (implementation TBD)
                                    }

                                    if json {
                                        printLogsAsJSON(message.logs)
                                    } else {
                                        printLogsAsText(message.logs)
                                    }
                                case .trailingMetadata:
                                    break
                                }
                            }
                        case .failure(let error):
                            throw error
                        }
                    }
                }
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                if !json {
                    Noora().warning("Connection lost, reconnecting...")
                }
                try await Task.sleep(for: .seconds(2))
            }
        }
    }

    private func parseSeverityLevel(_ level: String) -> Int32? {
        switch level.lowercased() {
        case "trace": return 1
        case "debug": return 5
        case "info": return 9
        case "warn", "warning": return 13
        case "error": return 17
        case "fatal": return 21
        default: return nil
        }
    }

    private func printLogsAsText(_ logs: Opentelemetry_Proto_Collector_Logs_V1_ExportLogsServiceRequest) {
        for resourceLogs in logs.resourceLogs {
            let serviceName = resourceLogs.resource.attributes
                .first { $0.key == "service.name" }?.value.stringValue ?? "unknown"

            for scopeLogs in resourceLogs.scopeLogs {
                for record in scopeLogs.logRecords {
                    let timestamp = formatTimestamp(record.timeUnixNano)
                    let severity = formatSeverity(record.severityNumber)
                    let body = record.body.stringValue

                    print("[\(timestamp)] [\(serviceName)] [\(severity)] \(body)")
                }
            }
        }
    }

    private func printLogsAsJSON(_ logs: Opentelemetry_Proto_Collector_Logs_V1_ExportLogsServiceRequest) {
        struct LogEntry: Codable {
            let timestamp: String
            let service: String
            let severity: String
            let body: String
        }

        for resourceLogs in logs.resourceLogs {
            let serviceName = resourceLogs.resource.attributes
                .first { $0.key == "service.name" }?.value.stringValue ?? "unknown"

            for scopeLogs in resourceLogs.scopeLogs {
                for record in scopeLogs.logRecords {
                    let entry = LogEntry(
                        timestamp: formatTimestamp(record.timeUnixNano),
                        service: serviceName,
                        severity: formatSeverity(record.severityNumber),
                        body: record.body.stringValue
                    )

                    if let data = try? JSONEncoder().encode(entry),
                       let json = String(data: data, encoding: .utf8) {
                        print(json)
                    }
                }
            }
        }
    }

    private func formatTimestamp(_ nanos: UInt64) -> String {
        let seconds = TimeInterval(nanos) / 1_000_000_000
        let date = Date(timeIntervalSince1970: seconds)
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: date)
    }

    private func formatSeverity(_ severity: Opentelemetry_Proto_Logs_V1_SeverityNumber) -> String {
        switch severity {
        case .trace, .trace2, .trace3, .trace4: return "TRACE"
        case .debug, .debug2, .debug3, .debug4: return "DEBUG"
        case .info, .info2, .info3, .info4: return "INFO"
        case .warn, .warn2, .warn3, .warn4: return "WARN"
        case .error, .error2, .error3, .error4: return "ERROR"
        case .fatal, .fatal2, .fatal3, .fatal4: return "FATAL"
        default: return "UNSPECIFIED"
        }
    }
}

struct MetricsCommand: AsyncParsableCommand {
    static let configuration = CommandConfiguration(
        commandName: "metrics",
        abstract: "Stream metrics from your device."
    )

    @Option(name: .shortAndLong, help: "Filter metrics by service name")
    var service: String?

    @Option(name: .long, help: "Filter metrics by app/container name")
    var app: String?

    @Option(name: .shortAndLong, help: "Filter metrics by name prefix")
    var prefix: String?

    @Flag(name: [.customShort("j"), .long], help: "Output in JSON format")
    var json: Bool = false

    @Option(name: .long, help: "Forward metrics to a local OTLP collector at this address (e.g., localhost:4317)")
    var forward: String?

    @OptionGroup var agentConnectionOptions: AgentConnectionOptions

    func run() async throws {
        let endpoint = try await agentConnectionOptions.read(
            title: "For which device do you want to stream metrics?"
        )

        if !json {
            Noora().info("Streaming metrics from device... (Press Ctrl+C to stop)")
        }

        // Reconnection loop - keeps trying to connect when agent restarts
        while !Task.isCancelled {
            do {
                try await withAgentGRPCClient(endpoint, title: "") { client in
                    let telemetry = Wendy_Agent_Services_V1_WendyTelemetryService.Client(wrapping: client)

                    let request = Wendy_Agent_Services_V1_StreamMetricsRequest.with {
                        if let service = service {
                            $0.serviceName = service
                        }
                        if let app = app {
                            $0.appName = app
                        }
                        if let prefix = prefix {
                            $0.metricNamePrefix = prefix
                        }
                    }

                    let shouldForward = forward != nil

                    try await telemetry.streamMetrics(request) { response in
                        switch response.accepted {
                        case .success(let contents):
                            for try await bodyPart in contents.bodyParts {
                                switch bodyPart {
                                case .message(let message):
                                    if shouldForward {
                                        // Forward to local collector (implementation TBD)
                                    }

                                    if json {
                                        printMetricsAsJSON(message.metrics)
                                    } else {
                                        printMetricsAsText(message.metrics)
                                    }
                                case .trailingMetadata:
                                    break
                                }
                            }
                        case .failure(let error):
                            throw error
                        }
                    }
                }
            } catch is CancellationError {
                throw CancellationError()
            } catch {
                if !json {
                    Noora().warning("Connection lost, reconnecting...")
                }
                try await Task.sleep(for: .seconds(2))
            }
        }
    }

    private func printMetricsAsText(_ metrics: Opentelemetry_Proto_Collector_Metrics_V1_ExportMetricsServiceRequest) {
        for resourceMetrics in metrics.resourceMetrics {
            let serviceName = resourceMetrics.resource.attributes
                .first { $0.key == "service.name" }?.value.stringValue ?? "unknown"

            for scopeMetrics in resourceMetrics.scopeMetrics {
                for metric in scopeMetrics.metrics {
                    let value = formatMetricValue(metric)
                    print("[\(serviceName)] \(metric.name): \(value)")
                }
            }
        }
    }

    private func printMetricsAsJSON(_ metrics: Opentelemetry_Proto_Collector_Metrics_V1_ExportMetricsServiceRequest) {
        struct MetricEntry: Codable {
            let service: String
            let name: String
            let value: String
            let unit: String
        }

        for resourceMetrics in metrics.resourceMetrics {
            let serviceName = resourceMetrics.resource.attributes
                .first { $0.key == "service.name" }?.value.stringValue ?? "unknown"

            for scopeMetrics in resourceMetrics.scopeMetrics {
                for metric in scopeMetrics.metrics {
                    let entry = MetricEntry(
                        service: serviceName,
                        name: metric.name,
                        value: formatMetricValue(metric),
                        unit: metric.unit
                    )

                    if let data = try? JSONEncoder().encode(entry),
                       let json = String(data: data, encoding: .utf8) {
                        print(json)
                    }
                }
            }
        }
    }

    private func formatMetricValue(_ metric: Opentelemetry_Proto_Metrics_V1_Metric) -> String {
        switch metric.data {
        case .gauge(let gauge):
            if let point = gauge.dataPoints.last {
                switch point.value {
                case .asDouble(let d): return String(format: "%.2f", d)
                case .asInt(let i): return String(i)
                default: return "N/A"
                }
            }
        case .sum(let sum):
            if let point = sum.dataPoints.last {
                switch point.value {
                case .asDouble(let d): return String(format: "%.2f", d)
                case .asInt(let i): return String(i)
                default: return "N/A"
                }
            }
        case .histogram(let histogram):
            if let point = histogram.dataPoints.last {
                return "count=\(point.count) sum=\(String(format: "%.2f", point.sum))"
            }
        case .summary(let summary):
            if let point = summary.dataPoints.last {
                return "count=\(point.count) sum=\(String(format: "%.2f", point.sum))"
            }
        default:
            break
        }
        return "N/A"
    }
}
