import Testing

@testable import WendyAgentCore

@Suite("OrgIdentity")
struct OrgIdentityTests {
    @Test("parses org from a well-formed common name")
    func parsesOrg() {
        #expect(OrgIdentity.organizationID(fromCommonName: "sh/wendy/7/42") == 7)
    }

    @Test("rejects malformed common names")
    func rejectsMalformed() {
        #expect(OrgIdentity.organizationID(fromCommonName: "sh/wendy/7") == nil)
        #expect(OrgIdentity.organizationID(fromCommonName: "nope") == nil)
        #expect(OrgIdentity.organizationID(fromCommonName: "sh/wendy/abc/42") == nil)
        #expect(OrgIdentity.organizationID(fromCommonName: "") == nil)
    }
}
