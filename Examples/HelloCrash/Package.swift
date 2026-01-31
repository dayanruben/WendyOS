// swift-tools-version: 6.0
// The swift-tools-version declares the minimum version of Swift required to build this package.

import PackageDescription

let package = Package(
    name: "HelloCrash",
    dependencies: [
        .package(path: "/Users/joannisorlandos/git/apple/swift-container-plugin")
    ],
    targets: [
        .executableTarget(
            name: "HelloCrash"
        )
    ]
)
