import Logging
import OpenTelemetryGRPC

actor OpenTelemetryProxy: Opentelemetry_Proto_Collector_Logs_V1_LogsService.SimpleServiceProtocol {
    let cloud: CloudClient
    let broadcaster: TelemetryBroadcaster
    let logger = Logger(label: "sh.wendy.agent.otel-proxy")

    init(cloud: CloudClient, broadcaster: TelemetryBroadcaster) {
        self.cloud = cloud
        self.broadcaster = broadcaster
    }

    func export(
        request: Opentelemetry_Proto_Collector_Logs_V1_ExportLogsServiceRequest,
        context: ServerContext
    ) async throws -> Opentelemetry_Proto_Collector_Logs_V1_ExportLogsServiceResponse {
        // Broadcast to local subscribers (CLI clients)
        await broadcaster.broadcastLogs(request)

        // Forward to cloud
        do {
            let otel = await Opentelemetry_Proto_Collector_Logs_V1_LogsService.Client(
                wrapping: cloud.grpcClient
            )
            return try await otel.export(request)
        } catch {
            logger.error("Error exporting logs: \(error)")
            return Opentelemetry_Proto_Collector_Logs_V1_ExportLogsServiceResponse()
        }
    }
}
