import Crypto
import Foundation

/// On-disk content store shared by the registry and the WriteLayer RPC.
/// Blobs live at `<root>/blobs/sha256/<hex>`; manifests at
/// `<root>/manifests/<repo>/<reference>`.
struct BlobStore: Sendable {
    let root: URL

    init(root: URL) {
        self.root = root
        try? FileManager.default.createDirectory(
            at: root.appendingPathComponent("blobs/sha256"),
            withIntermediateDirectories: true
        )
    }

    enum BlobError: Error {
        case digestMismatch(expected: String, actual: String)
        case badDigest(String)
    }

    func blobURL(digest: String) -> URL {
        root.appendingPathComponent("blobs/\(digest.replacingOccurrences(of: ":", with: "/"))")
    }

    func hasBlob(digest: String) -> Bool {
        FileManager.default.fileExists(atPath: blobURL(digest: digest).path)
    }

    func writeBlob(_ data: Data, expectedDigest: String) throws {
        let hex = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
        let actual = "sha256:\(hex)"
        guard actual == expectedDigest.lowercased() else {
            throw BlobError.digestMismatch(expected: expectedDigest, actual: actual)
        }
        let url = blobURL(digest: actual)
        try FileManager.default.createDirectory(
            at: url.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        try data.write(to: url, options: .atomic)
    }

    func manifestURL(repository: String, reference: String) -> URL? {
        let url = manifestPath(repository: repository, reference: reference)
        return FileManager.default.fileExists(atPath: url.path) ? url : nil
    }

    func writeManifest(_ data: Data, repository: String, reference: String) throws {
        let url = manifestPath(repository: repository, reference: reference)
        try FileManager.default.createDirectory(
            at: url.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        try data.write(to: url, options: .atomic)
        // Also index by content digest so pulls by digest resolve.
        let hex = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
        let byDigest = manifestPath(repository: repository, reference: "sha256:\(hex)")
        try? data.write(to: byDigest, options: .atomic)
    }

    private func manifestPath(repository: String, reference: String) -> URL {
        root.appendingPathComponent("manifests")
            .appendingPathComponent(repository)
            .appendingPathComponent(reference.replacingOccurrences(of: ":", with: "_"))
    }
}
