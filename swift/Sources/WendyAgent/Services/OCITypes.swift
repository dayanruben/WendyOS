import Foundation

struct OCIManifest: Codable {
    let schemaVersion: Int
    let config: OCIDescriptor
    let layers: [OCIDescriptor]
}

struct OCIDescriptor: Codable {
    let mediaType: String
    let digest: String
    let size: Int64
}

struct OCIImageConfig: Codable {
    let architecture: String?
    let os: String?
    let config: OCIContainerConfig?
    let rootfs: OCIRootFS?
}

struct OCIContainerConfig: Codable {
    // OCI spec uses capital-letter keys for these fields.
    let Entrypoint: [String]?
    let Cmd: [String]?
    let WorkingDir: String?
    let Env: [String]?
}

struct OCIRootFS: Codable {
    let type: String
    let diff_ids: [String]
}
