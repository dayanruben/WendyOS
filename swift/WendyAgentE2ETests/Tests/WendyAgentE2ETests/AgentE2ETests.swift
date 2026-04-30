import Testing
import WendyE2ETesting

@Suite(.serialized)
struct `agent e2e` {

    @Test(.timeLimit(.minutes(10)))
    func `build CLI and agent`() async throws {
        let cli = try await Machine.cli()
        let agent = try await Machine.agent()

        #expect(cli.name == "CLI")
        #expect(agent.name == "Agent")
    }
}
