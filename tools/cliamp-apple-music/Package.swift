// swift-tools-version: 5.9

import PackageDescription

let package = Package(
    name: "cliamp-apple-music",
    platforms: [.macOS(.v14)],
    targets: [
        .executableTarget(
            name: "cliamp-apple-music",
            path: "Sources",
            linkerSettings: [
                .unsafeFlags([
                    "-Xlinker", "-sectcreate",
                    "-Xlinker", "__TEXT",
                    "-Xlinker", "__info_plist",
                    "-Xlinker", "Resources/Info.plist",
                ])
            ]
        ),
    ]
)
