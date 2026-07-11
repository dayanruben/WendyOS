import Crypto
import Foundation
import Testing

@testable import WendyAgentCore

@Suite struct BlobStoreTests {
    private func tempRoot() -> URL {
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("blobstore-\(UUID().uuidString)")
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    @Test func writeAndReadBlobByDigest() throws {
        let store = BlobStore(root: tempRoot())
        let payload = Data("hello".utf8)
        let hex = SHA256.hash(data: payload).map { String(format: "%02x", $0) }.joined()
        let digest = "sha256:\(hex)"
        try store.writeBlob(payload, expectedDigest: digest)
        #expect(store.hasBlob(digest: digest))
        #expect(try Data(contentsOf: store.blobURL(digest: digest)) == payload)
    }

    @Test func writeBlobRejectsDigestMismatch() {
        let store = BlobStore(root: tempRoot())
        #expect(throws: (any Error).self) {
            try store.writeBlob(Data("hello".utf8), expectedDigest: "sha256:deadbeef")
        }
    }

    @Test func manifestRoundTripByTag() throws {
        let store = BlobStore(root: tempRoot())
        let manifest = Data(#"{"schemaVersion":2}"#.utf8)
        try store.writeManifest(manifest, repository: "app", reference: "latest")
        let url = try #require(store.manifestURL(repository: "app", reference: "latest"))
        #expect(try Data(contentsOf: url) == manifest)
    }

    @Test func manifestRoundTripByContentDigest() throws {
        let store = BlobStore(root: tempRoot())
        let manifest = Data(#"{"schemaVersion":2}"#.utf8)
        try store.writeManifest(manifest, repository: "app", reference: "latest")
        let hex = SHA256.hash(data: manifest).map { String(format: "%02x", $0) }.joined()
        let url = try #require(store.manifestURL(repository: "app", reference: "sha256:\(hex)"))
        #expect(try Data(contentsOf: url) == manifest)
    }
}
