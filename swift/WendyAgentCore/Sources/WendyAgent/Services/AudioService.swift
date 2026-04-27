import GRPCCore
import WendyAgentGRPC

struct AudioService: Wendy_Agent_Services_V1_WendyAudioService.ServiceProtocol {
    func listAudioDevices(
        request: ServerRequest<Wendy_Agent_Services_V1_ListAudioDevicesRequest>,
        context: ServerContext
    ) async throws -> ServerResponse<Wendy_Agent_Services_V1_ListAudioDevicesResponse> {
        throw UnsupportedRPC.error()
    }

    func setDefaultAudioDevice(
        request: ServerRequest<Wendy_Agent_Services_V1_SetDefaultAudioDeviceRequest>,
        context: ServerContext
    ) async throws -> ServerResponse<Wendy_Agent_Services_V1_SetDefaultAudioDeviceResponse> {
        throw UnsupportedRPC.error()
    }

    func streamAudioLevels(
        request: ServerRequest<Wendy_Agent_Services_V1_StreamAudioLevelsRequest>,
        context: ServerContext
    ) async throws -> StreamingServerResponse<Wendy_Agent_Services_V1_AudioLevelUpdate> {
        throw UnsupportedRPC.error()
    }

    func streamAudio(
        request: ServerRequest<Wendy_Agent_Services_V1_StreamAudioRequest>,
        context: ServerContext
    ) async throws -> StreamingServerResponse<Wendy_Agent_Services_V1_AudioChunk> {
        throw UnsupportedRPC.error()
    }
}
