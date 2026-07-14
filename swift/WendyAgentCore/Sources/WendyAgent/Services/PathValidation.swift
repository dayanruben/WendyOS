public import Foundation

/// Error thrown when an untrusted path component would escape its base directory.
public enum PathValidationError: Error, Equatable {
    case unsafePath(String)
}

/// Joins `relative` onto `base` and guarantees the standardized result stays
/// contained within `base`. Rejects empty input, absolute paths, `..` traversal,
/// and any result that escapes `base`. Used for every RPC-supplied path component
/// (app name, file name, blob digest, app id) before a filesystem read or write.
///
/// Mirrors the existing `ContainerService.removeNativeAppDirectory` containment
/// check (standardized-path prefix), generalized for reuse.
public func validateContainedPath(base: URL, relative: String) throws -> URL {
    guard !relative.isEmpty, !relative.hasPrefix("/") else {
        throw PathValidationError.unsafePath(relative)
    }
    let baseStd = base.standardizedFileURL
    let candidate = baseStd.appendingPathComponent(relative).standardizedFileURL
    guard candidate.path == baseStd.path || candidate.path.hasPrefix(baseStd.path + "/") else {
        throw PathValidationError.unsafePath(relative)
    }
    return candidate
}
