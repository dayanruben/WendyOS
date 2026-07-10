import Foundation
import GRPCCore
import Testing
import WendyAgentGRPC

@testable import WendyAgentCore

@Suite("ProvisioningService")
struct ProvisioningServiceTests {
    @Test("unprovision succeeds idempotently and device stays not provisioned")
    func unprovisionSucceeds() async throws {
        let service = ProvisioningService()
        let context = makeProvisioningContext(method: "Unprovision")

        // Unprovision does not throw and returns a response.
        _ = try await service.unprovision(
            request: Wendy_Agent_Services_V1_UnprovisionRequest(),
            context: context
        )

        // The device remains honestly not-provisioned.
        let status = try await service.isProvisioned(
            request: Wendy_Agent_Services_V1_IsProvisionedRequest(),
            context: makeProvisioningContext(method: "IsProvisioned")
        )
        guard case .notProvisioned = status.response else {
            Issue.record("expected notProvisioned, got \(String(describing: status.response))")
            return
        }
    }
}

private func makeProvisioningContext(method: String) -> ServerContext {
    ServerContext(
        descriptor: MethodDescriptor(
            fullyQualifiedService: "wendy.agent.services.v1.WendyProvisioningService",
            method: method
        ),
        remotePeer: "in-process:test",
        localPeer: "in-process:test",
        cancellation: .init()
    )
}
