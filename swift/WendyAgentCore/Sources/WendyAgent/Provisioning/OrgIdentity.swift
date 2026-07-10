import Foundation
import X509

/// Parses the Wendy device organization out of a certificate common name of the
/// form `sh/wendy/<org>/<asset>`. Used by the mTLS org-equality check.
enum OrgIdentity {
    static func organizationID(fromCommonName cn: String) -> Int32? {
        let parts = cn.split(separator: "/", omittingEmptySubsequences: false)
        // Expect exactly ["sh", "wendy", "<org>", "<asset>"].
        guard parts.count == 4, parts[0] == "sh", parts[1] == "wendy" else { return nil }
        return Int32(parts[2])
    }

    static func organizationID(fromLeaf certificate: Certificate) -> Int32? {
        for relativeName in certificate.subject {
            for attribute in relativeName where attribute.type == .RDNAttributeType.commonName {
                if let org = self.organizationID(fromCommonName: attribute.value.description) {
                    return org
                }
            }
        }
        return nil
    }
}
