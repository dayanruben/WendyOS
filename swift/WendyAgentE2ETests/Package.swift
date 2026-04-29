// swift-tools-version: 6.2.0
import PackageDescription

let package = Package(
    name: "WendyE2ETesting",
    platforms: [
        .macOS(.v15)
    ],
    products: [
        .library(name: "WendyE2ETesting", targets: ["WendyE2ETesting"]),
    ],
    dependencies: [
        .package(url: "https://github.com/swiftlang/swift-subprocess.git", from: "0.4.0"),
        .package(url: "https://github.com/apple/swift-system", from: "1.6.0"),
    ],
    targets: [
        .target(
            name: "WendyE2ETesting",
            dependencies: [
                .product(name: "Subprocess", package: "swift-subprocess"),
                .product(name: "SystemPackage", package: "swift-system"),
            ],
            path: "Sources/WendyE2ETesting"
        ),
        .testTarget(
            name: "WendyE2ETestingTests",
            dependencies: ["WendyE2ETesting"],
            path: "Tests/WendyE2ETestingTests"
        ),
    ]
)
