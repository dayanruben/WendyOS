import Foundation
import SwiftASN1
import X509

/// Decides whether an incoming mTLS client certificate chain may talk to this
/// device's main gRPC server. This is the security core of the agent's mTLS
/// path: it (1) verifies the peer-presented chain against the device's own CA
/// trust roots — the same chain the cloud issued the device — and then (2)
/// enforces org-equality, rejecting a client whose organization differs from
/// this device's.
///
/// This runs INSIDE NIOSSL's custom verification callback, which fully replaces
/// BoringSSL's built-in verification (see `NIOSSLCustomVerificationCallbackWithMetadata`:
/// "Setting this callback will override _all_ verification logic that BoringSSL
/// provides"). Therefore this function is solely responsible for building a
/// verified path to a trusted root; it never assumes the peer-presented
/// certificates are valid. It fails closed: any parse or verification failure
/// returns `false`.
enum ClientCertAuthorizer {
    /// - Parameters:
    ///   - peerCertificatesDER: The peer-presented certificate chain, DER-encoded,
    ///     leaf first (the order NIOSSL delivers them in).
    ///   - trustRootsPEM: The device's CA chain (PEM), used as the trust anchors.
    ///   - deviceOrg: This device's organization id, or `nil` if it could not be
    ///     determined from the device's own certificate. When `nil`, every client
    ///     is rejected (fail closed): org-equality is the sole cross-org barrier
    ///     and is never silently dropped.
    /// - Returns: `true` iff the chain verifies to a trusted root AND the device
    ///   org is known AND the client's org equals the device's org.
    static func isAuthorized(
        peerCertificatesDER: [[UInt8]],
        trustRootsPEM: String,
        deviceOrg: Int32?
    ) async -> Bool {
        // Parse trust anchors from the device CA chain. Without any anchors we
        // cannot verify a path, so fail closed.
        let roots = Self.parseCertificates(pem: trustRootsPEM)
        guard !roots.isEmpty else { return false }

        // Parse the peer-presented chain: leaf first, remainder are intermediates.
        let peerCerts = peerCertificatesDER.compactMap { try? Certificate(derEncoded: $0) }
        guard let leaf = peerCerts.first else { return false }
        let intermediates = Array(peerCerts.dropFirst())

        // Mandatory: build a verified path from the leaf to a trusted root.
        var verifier = Verifier(rootCertificates: CertificateStore(roots)) {
            RFC5280Policy()
        }
        let result = await verifier.validate(
            leaf: leaf,
            intermediates: CertificateStore(intermediates)
        )
        guard case .validCertificate = result else { return false }

        // Additional layer: org-equality. Because the PKI shares CA roots
        // across organizations, chain verification alone would let a validly
        // provisioned device from another org connect — org-equality is the
        // sole cross-org barrier, so it fails closed. If the device's own org
        // is unknown we reject every client rather than silently dropping that
        // barrier (a device with an unparseable cert becomes unreachable over
        // mTLS, which is the safe failure mode).
        guard let deviceOrg else { return false }
        guard let clientOrg = OrgIdentity.organizationID(fromLeaf: leaf) else {
            return false
        }
        return clientOrg == deviceOrg
    }

    /// The organization id encoded in the common name of the leaf certificate of
    /// the given PEM, or `nil` if it can't be parsed. Used to derive the device's
    /// own org from its issued certificate when building the mTLS server.
    static func organizationID(fromLeafPEM pem: String) -> Int32? {
        guard let leaf = Self.parseCertificates(pem: pem).first else { return nil }
        return OrgIdentity.organizationID(fromLeaf: leaf)
    }

    /// Parses zero or more PEM `CERTIFICATE` blocks into certificates, ignoring
    /// any block that isn't a certificate or that fails to parse.
    static func parseCertificates(pem: String) -> [Certificate] {
        guard let documents = try? PEMDocument.parseMultiple(pemString: pem) else {
            return []
        }
        return documents.compactMap { try? Certificate(pemDocument: $0) }
    }
}
