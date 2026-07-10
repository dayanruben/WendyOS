import Crypto
import Foundation
import SwiftASN1
import Testing
import X509

@testable import WendyAgentCore

@Suite("ClientCertAuthorizer")
struct ClientCertAuthorizerTests {
    /// A self-signed CA and its private key, for issuing leaf certificates.
    private struct TestCA {
        var certificate: Certificate
        var privateKey: Certificate.PrivateKey
        var pem: String
    }

    private static func makeCA(commonName: String = "Wendy Test CA") throws -> TestCA {
        let key = P256.Signing.PrivateKey()
        let name = try DistinguishedName { CommonName(commonName) }
        let cert = try Certificate(
            version: .v3,
            serialNumber: Certificate.SerialNumber(),
            publicKey: Certificate.PublicKey(key.publicKey),
            notValidBefore: Date().addingTimeInterval(-3600),
            notValidAfter: Date().addingTimeInterval(3600),
            issuer: name,
            subject: name,
            signatureAlgorithm: .ecdsaWithSHA256,
            extensions: try Certificate.Extensions {
                Critical(BasicConstraints.isCertificateAuthority(maxPathLength: nil))
                Critical(KeyUsage(keyCertSign: true))
            },
            issuerPrivateKey: Certificate.PrivateKey(key)
        )
        return TestCA(
            certificate: cert,
            privateKey: Certificate.PrivateKey(key),
            pem: try cert.serializeAsPEM().pemString
        )
    }

    private static func makeLeaf(org: Int32, asset: Int32, ca: TestCA) throws -> Certificate {
        let key = P256.Signing.PrivateKey()
        let subject = try DistinguishedName {
            CommonName(DeviceIdentity.commonName(organizationID: org, assetID: asset))
        }
        return try Certificate(
            version: .v3,
            serialNumber: Certificate.SerialNumber(),
            publicKey: Certificate.PublicKey(key.publicKey),
            notValidBefore: Date().addingTimeInterval(-3600),
            notValidAfter: Date().addingTimeInterval(3600),
            issuer: ca.certificate.subject,
            subject: subject,
            signatureAlgorithm: .ecdsaWithSHA256,
            extensions: try Certificate.Extensions {
                Critical(BasicConstraints.notCertificateAuthority)
                Critical(KeyUsage(digitalSignature: true))
                try ExtendedKeyUsage([.clientAuth, .serverAuth])
            },
            issuerPrivateKey: ca.privateKey
        )
    }

    private static func der(_ certificate: Certificate) throws -> [UInt8] {
        var serializer = DER.Serializer()
        try serializer.serialize(certificate)
        return serializer.serializedBytes
    }

    @Test("accepts a CA-signed client whose org matches the device org")
    func acceptsMatchingOrg() async throws {
        let ca = try Self.makeCA()
        let client = try Self.makeLeaf(org: 7, asset: 42, ca: ca)

        let authorized = await ClientCertAuthorizer.isAuthorized(
            peerCertificatesDER: [try Self.der(client)],
            trustRootsPEM: ca.pem,
            deviceOrg: 7
        )
        #expect(authorized)
    }

    @Test("rejects a CA-signed client whose org differs from the device org")
    func rejectsMismatchedOrg() async throws {
        let ca = try Self.makeCA()
        let client = try Self.makeLeaf(org: 99, asset: 1, ca: ca)

        let authorized = await ClientCertAuthorizer.isAuthorized(
            peerCertificatesDER: [try Self.der(client)],
            trustRootsPEM: ca.pem,
            deviceOrg: 7
        )
        #expect(!authorized)
    }

    @Test("rejects a client not signed by the device CA even if the org matches")
    func rejectsUntrustedChain() async throws {
        let deviceCA = try Self.makeCA(commonName: "Wendy Device CA")
        let rogueCA = try Self.makeCA(commonName: "Rogue CA")
        // Same org number, but signed by a CA the device does not trust.
        let client = try Self.makeLeaf(org: 7, asset: 42, ca: rogueCA)

        let authorized = await ClientCertAuthorizer.isAuthorized(
            peerCertificatesDER: [try Self.der(client)],
            trustRootsPEM: deviceCA.pem,
            deviceOrg: 7
        )
        #expect(!authorized)
    }

    @Test("with device org unknown, still requires a verified chain")
    func orgEnforcementDisabledStillVerifiesChain() async throws {
        let ca = try Self.makeCA()
        let rogueCA = try Self.makeCA(commonName: "Rogue CA")
        let trusted = try Self.makeLeaf(org: 3, asset: 9, ca: ca)
        let untrusted = try Self.makeLeaf(org: 3, asset: 9, ca: rogueCA)

        let allowsTrusted = await ClientCertAuthorizer.isAuthorized(
            peerCertificatesDER: [try Self.der(trusted)],
            trustRootsPEM: ca.pem,
            deviceOrg: nil
        )
        #expect(allowsTrusted)

        let rejectsUntrusted = await ClientCertAuthorizer.isAuthorized(
            peerCertificatesDER: [try Self.der(untrusted)],
            trustRootsPEM: ca.pem,
            deviceOrg: nil
        )
        #expect(!rejectsUntrusted)
    }

    @Test("rejects an empty peer chain")
    func rejectsEmptyPeerChain() async throws {
        let ca = try Self.makeCA()
        let authorized = await ClientCertAuthorizer.isAuthorized(
            peerCertificatesDER: [],
            trustRootsPEM: ca.pem,
            deviceOrg: 7
        )
        #expect(!authorized)
    }

    @Test("rejects when there are no trust roots")
    func rejectsNoTrustRoots() async throws {
        let ca = try Self.makeCA()
        let client = try Self.makeLeaf(org: 7, asset: 42, ca: ca)
        let authorized = await ClientCertAuthorizer.isAuthorized(
            peerCertificatesDER: [try Self.der(client)],
            trustRootsPEM: "",
            deviceOrg: 7
        )
        #expect(!authorized)
    }

    @Test("derives the device org from its own leaf PEM")
    func derivesDeviceOrgFromLeafPEM() throws {
        let ca = try Self.makeCA()
        let leaf = try Self.makeLeaf(org: 12, asset: 34, ca: ca)
        let pem = try leaf.serializeAsPEM().pemString
        #expect(ClientCertAuthorizer.organizationID(fromLeafPEM: pem) == 12)
    }
}
