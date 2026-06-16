import ArgumentParser
import Foundation

struct ReviewCommand: AsyncParsableCommand {
    static let configuration = CommandConfiguration(
        commandName: "review",
        abstract: "Review a Swift E2E run with a single AI review pass."
    )

    @Option(name: .long, help: "Swift package directory.")
    var packageDir = "."

    @Option(name: .long, help: "Directory containing Swift E2E test sources.")
    var testsDir: String?

    @Option(
        name: .long,
        help: "E2E run directory. Reads run results and writes AI review files."
    )
    var runDir: String

    @Option(name: .long, help: "AI review harness: auto, claude, or codex.")
    var harness: ReviewHarnessPreference?

    @Option(name: .long, help: "Review prompt path.")
    var reviewPrompt: String?

    @Option(
        name: .long,
        help: "Git diff range for diff-scoped review, for example origin/main...HEAD."
    )
    var diff: String?

    @Flag(name: .long, help: "Overwrite existing review files.")
    var overwrite = false

    mutating func run() async throws {
        let packageURL = URL(fileURLWithPath: packageDir).standardizedFileURL
        let testsURL = URL(
            fileURLWithPath: testsDir ?? defaultReviewTestsDir(packageURL: packageURL).path
        ).standardizedFileURL
        let runURL = URL(fileURLWithPath: runDir, isDirectory: true).standardizedFileURL
        let repoURL = packageURL.deletingLastPathComponent().deletingLastPathComponent()
            .standardizedFileURL

        guard try isReviewRunDirectory(runURL) else {
            throw ValidationError(
                "Swift E2E review expects a Swift E2E run directory. Run E2EAggregate first, then pass the run directory to --run-dir."
            )
        }

        try await runReview(
            packageURL: packageURL,
            testsURL: testsURL,
            runURL: runURL,
            repoURL: repoURL
        )
    }
}

extension ReviewCommand {
    fileprivate func runReview(
        packageURL: URL,
        testsURL: URL,
        runURL: URL,
        repoURL: URL
    ) async throws {
        let reviewMode = try ReviewMode(diff: diff)
        let context = try reviewMode.prepareContext(runURL: runURL, repoURL: repoURL)
        let promptURL = URL(
            fileURLWithPath: reviewPrompt
                ?? packageURL.appendingPathComponent("Support/e2e-review.prompt.md").path
        ).standardizedFileURL
        let basePrompt = try String(contentsOf: promptURL, encoding: .utf8)
        let reviewHarness = try makeReviewHarness(preference: harness)
        let resolvedModel = reviewHarness.modelName
        let reviewer = e2eReviewReviewer(model: resolvedModel)
        let reviewDirectoryName = e2eReviewDirectoryName(reviewer: reviewer)
        let overview = try ensureRunOverview(in: runURL)

        print("==> Running Swift E2E run AI review")
        print("    Harness:        \(reviewHarness.harnessName)")
        print("    Command source: \(reviewHarness.commandSource)")
        print("    Invocation:     \(reviewHarness.invocationSummary)")
        print("    Model:          \(resolvedModel)")
        print("    Model source:   \(reviewHarness.modelSource)")
        print("    Auth:           \(reviewHarness.authSummary)")
        print("    Repo:           \(repoURL.path)")
        print("    Run:            \(runURL.path)")
        print("    Overview:       \(runOverviewURL(in: runURL).path)")
        print("    Mode:           \(reviewMode.name)")
        print("    Reviewer:       \(reviewer)")
        print("    Review dir:     \(reviewDirectoryName)")
        if let range = reviewMode.diffRange {
            print("    Diff:           \(range)")
        }
        if let changedFilesURL = context.changedFilesURL {
            print("    Name-only diff: \(changedFilesURL.path)")
        }
        if let diffstatURL = context.diffstatURL {
            print("    Diff stat:      \(diffstatURL.path)")
        }
        print("    Prompt:         \(promptURL.path)")

        if overwrite {
            try removeExistingRunReviews(in: runURL, reviewer: reviewer)
        }

        let prompt = try runReviewPrompt(
            basePrompt: basePrompt,
            repoURL: repoURL,
            packageURL: packageURL,
            testsURL: testsURL,
            runURL: runURL,
            context: context,
            overview: overview,
            reviewer: reviewer,
            reviewDirectoryName: reviewDirectoryName,
            overwrite: overwrite
        )
        try reviewHarness.review(
            prompt: prompt,
            repoURL: repoURL,
            runURL: runURL
        )
        try enforceRunReportReviewContract(in: runURL, reviewer: reviewer)
        print("==> Swift E2E run AI review complete")

    }
}

private enum ReviewMode {
    case full
    case diff(String)

    init(diff: String?) throws {
        guard let diff else {
            self = .full
            return
        }
        let trimmed = diff.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            throw ValidationError("--diff must not be empty.")
        }
        self = .diff(trimmed)
    }

    var name: String {
        switch self {
        case .full:
            "full"
        case .diff:
            "diff"
        }
    }

    var diffRange: String? {
        if case .diff(let range) = self { range } else { nil }
    }

    func prepareContext(runURL: URL, repoURL: URL) throws -> ReviewContext {
        switch self {
        case .full:
            return ReviewContext(mode: self, changedFilesURL: nil, diffstatURL: nil)
        case .diff(let range):
            let changedFilesURL = runURL.appendingPathComponent("git-diff-name-only.txt")
            let diffstatURL = runURL.appendingPathComponent("git-diff-stat.txt")

            try runGitDiffContext(
                arguments: ["diff", "--name-only", range],
                outputURL: changedFilesURL,
                repoURL: repoURL,
                diffRange: range
            )
            try runGitDiffContext(
                arguments: ["diff", "--stat", range],
                outputURL: diffstatURL,
                repoURL: repoURL,
                diffRange: range
            )
            return ReviewContext(
                mode: self,
                changedFilesURL: changedFilesURL,
                diffstatURL: diffstatURL
            )
        }
    }
}

private struct ReviewContext {
    var mode: ReviewMode
    var changedFilesURL: URL?
    var diffstatURL: URL?
}

private struct RunReviewAttemptArtifact {
    var target: String
    var attempt: String
    var exitStatus: Int?
    var observationCount: Int
    var files: [String]

    var needsDiagnosis: Bool {
        if let exitStatus, exitStatus != 0 { return true }
        return observationCount == 0
    }
}

private struct ShellReviewHarness: Sendable {
    var harnessName: String
    var modelName: String
    var shellCommand: String
    var commandSource: String
    var invocationSummary: String
    var authSummary: String
    var modelSource: String

    func review(prompt: String, repoURL: URL, runURL: URL) throws {
        let promptURL = runURL.appendingPathComponent(
            ".review-harness-prompt-\(UUID().uuidString).md"
        )
        try prompt.write(to: promptURL, atomically: true, encoding: .utf8)
        defer { try? FileManager.default.removeItem(at: promptURL) }

        var environment = ProcessInfo.processInfo.environment
        environment["WENDY_E2E_REVIEW_PROMPT"] = promptURL.path
        environment["WENDY_E2E_REVIEW_RUN_DIR"] = runURL.path
        environment["WENDY_E2E_REVIEW_REPO_DIR"] = repoURL.path

        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/bash")
        process.arguments = ["-lc", shellCommand]
        process.currentDirectoryURL = repoURL
        process.environment = environment
        process.standardInput = FileHandle.nullDevice

        try process.run()
        process.waitUntilExit()

        guard process.terminationStatus == 0 else {
            throw ValidationError(
                "Review harness \(harnessName) failed with exit status \(process.terminationStatus)."
            )
        }
    }
}

enum ReviewHarnessPreference: String, ExpressibleByArgument {
    case auto
    case claude
    case codex

    init?(argument: String) {
        self.init(rawValue: argument.lowercased())
    }
}

private enum ReviewHarnessModel {
    static let defaultCodex = "gpt-5.5"
    static let defaultClaude = "claude-opus-4-7"
}

private struct ResolvedReviewHarnessModel {
    var name: String
    var source: String
}

private func makeReviewHarness(
    preference explicitPreference: ReviewHarnessPreference?
) throws -> ShellReviewHarness {
    let environment = ProcessInfo.processInfo.environment
    let hasCodex = hasExecutable("codex")
    let hasClaude = hasExecutable("claude")
    let hasOpenAIAPIKey = !environment["OPENAI_API_KEY", default: ""].isEmpty
    let hasAnthropicAPIKey = !environment["ANTHROPIC_API_KEY", default: ""].isEmpty
    let preference = explicitPreference ?? reviewHarnessPreference(environment: environment)
    let codexModel = reviewHarnessModel(
        defaultName: ReviewHarnessModel.defaultCodex,
        environmentName: "WENDY_E2E_REVIEW_CODEX_MODEL",
        environment: environment
    )
    let claudeModel = reviewHarnessModel(
        defaultName: ReviewHarnessModel.defaultClaude,
        environmentName: "WENDY_E2E_REVIEW_CLAUDE_MODEL",
        environment: environment
    )

    switch preference {
    case .auto:
        if hasClaude && claudeCodeSubscriptionConfigured() {
            return claudeHarness(
                model: claudeModel,
                authSummary: "Claude Code subscription",
                apiKeyOnly: false
            )
        }
        if hasCodex && codexSubscriptionConfigured() {
            return codexHarness(model: codexModel, authSummary: "Codex subscription")
        }
        if hasClaude && hasAnthropicAPIKey {
            return claudeHarness(
                model: claudeModel,
                authSummary: "ANTHROPIC_API_KEY",
                apiKeyOnly: true
            )
        }
        if hasCodex && hasOpenAIAPIKey {
            return codexHarness(model: codexModel, authSummary: "OPENAI_API_KEY")
        }
    case .claude:
        if hasClaude && claudeCodeSubscriptionConfigured() {
            return claudeHarness(
                model: claudeModel,
                authSummary: "Claude Code subscription",
                apiKeyOnly: false
            )
        }
        if hasClaude && hasAnthropicAPIKey {
            return claudeHarness(
                model: claudeModel,
                authSummary: "ANTHROPIC_API_KEY",
                apiKeyOnly: true
            )
        }
    case .codex:
        if hasCodex && codexSubscriptionConfigured() {
            return codexHarness(model: codexModel, authSummary: "Codex subscription")
        }
        if hasCodex && hasOpenAIAPIKey {
            return codexHarness(model: codexModel, authSummary: "OPENAI_API_KEY")
        }
    }

    throw ValidationError(reviewHarnessErrorMessage(preference: preference))
}

private func reviewHarnessPreference(environment: [String: String]) -> ReviewHarnessPreference {
    let value = environment["WENDY_E2E_REVIEW_HARNESS", default: ""]
    return ReviewHarnessPreference(argument: value) ?? .auto
}

private func reviewHarnessModel(
    defaultName: String,
    environmentName: String,
    environment: [String: String]
) -> ResolvedReviewHarnessModel {
    let value = environment[environmentName, default: ""]
        .trimmingCharacters(in: .whitespacesAndNewlines)
    guard !value.isEmpty else {
        return ResolvedReviewHarnessModel(name: defaultName, source: "default")
    }
    return ResolvedReviewHarnessModel(name: value, source: environmentName)
}

private func reviewHarnessErrorMessage(preference: ReviewHarnessPreference) -> String {
    switch preference {
    case .auto:
        return
            "Swift E2E review requires Codex or Claude Code. Configure Codex subscription auth, Claude Code subscription auth, ANTHROPIC_API_KEY with Claude Code, or OPENAI_API_KEY with Codex."
    case .claude:
        return
            "Swift E2E review was forced to Claude Code, but Claude Code is not usable. Configure Claude Code subscription auth or ANTHROPIC_API_KEY."
    case .codex:
        return
            "Swift E2E review was forced to Codex, but Codex is not usable. Configure Codex subscription auth or OPENAI_API_KEY."
    }
}

private func codexHarness(
    model: ResolvedReviewHarnessModel,
    authSummary: String
) -> ShellReviewHarness {
    ShellReviewHarness(
        harnessName: "codex",
        modelName: model.name,
        shellCommand:
            #"prompt="Read and follow the E2E review instructions in $WENDY_E2E_REVIEW_PROMPT."; codex exec --color never --dangerously-bypass-approvals-and-sandbox --model \#(shellQuoted(model.name)) -c model_reasoning_effort="high" "$prompt""#,
        commandSource: "codex CLI",
        invocationSummary:
            "codex exec --color never --dangerously-bypass-approvals-and-sandbox --model \(model.name) -c model_reasoning_effort=high <generated prompt>",
        authSummary: authSummary,
        modelSource: model.source
    )
}

private func claudeHarness(
    model: ResolvedReviewHarnessModel,
    authSummary: String,
    apiKeyOnly: Bool
) -> ShellReviewHarness {
    let bareFlag = apiKeyOnly ? " --bare" : ""
    return ShellReviewHarness(
        harnessName: "claude",
        modelName: model.name,
        shellCommand: apiKeyOnly
            ? #"prompt="Read and follow the E2E review instructions in $WENDY_E2E_REVIEW_PROMPT."; claude --bare --model \#(shellQuoted(model.name)) --effort high --dangerously-skip-permissions --print "$prompt""#
            : #"prompt="Read and follow the E2E review instructions in $WENDY_E2E_REVIEW_PROMPT."; claude --model \#(shellQuoted(model.name)) --effort high --dangerously-skip-permissions --print "$prompt""#,
        commandSource: "Claude Code CLI",
        invocationSummary:
            "claude\(bareFlag) --model \(model.name) --effort high --dangerously-skip-permissions --print <generated prompt>",
        authSummary: authSummary,
        modelSource: model.source
    )
}

private func shellQuoted(_ value: String) -> String {
    "'" + value.replacingOccurrences(of: "'", with: "'\\''") + "'"
}

private func hasExecutable(_ name: String) -> Bool {
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    process.arguments = ["which", name]
    process.standardOutput = FileHandle.nullDevice
    process.standardError = FileHandle.nullDevice
    do {
        try process.run()
        process.waitUntilExit()
        return process.terminationStatus == 0
    } catch {
        return false
    }
}

private struct CommandOutput {
    var status: Int32
    var text: String
}

private func commandOutput(_ arguments: [String]) -> CommandOutput? {
    let pipe = Pipe()
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    process.arguments = arguments
    process.standardOutput = pipe
    process.standardError = pipe
    process.standardInput = FileHandle.nullDevice
    do {
        try process.run()
        process.waitUntilExit()
        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        return CommandOutput(
            status: process.terminationStatus,
            text: String(data: data, encoding: .utf8) ?? ""
        )
    } catch {
        return nil
    }
}

private func codexSubscriptionConfigured() -> Bool {
    guard let output = commandOutput(["codex", "login", "status"]) else {
        return false
    }
    return output.status == 0 && !output.text.localizedCaseInsensitiveContains("not logged in")
}

private func claudeCodeSubscriptionConfigured() -> Bool {
    if claudeCredentialsContainSubscriptionAuth() {
        return true
    }

    if let output = commandOutput(["claude", "auth", "status"]),
        output.status == 0,
        let data = output.text.data(using: .utf8),
        let object = try? JSONSerialization.jsonObject(with: data),
        let status = object as? [String: Any],
        let loggedIn = status["loggedIn"] as? Bool,
        loggedIn,
        let authMethod = status["authMethod"] as? String,
        authMethod == "claude.ai"
    {
        return true
    }

    return false
}

private func claudeCredentialsContainSubscriptionAuth() -> Bool {
    let credentialsPath = "\(NSHomeDirectory())/.claude/.credentials.json"
    guard let data = FileManager.default.contents(atPath: credentialsPath),
        let object = try? JSONSerialization.jsonObject(with: data),
        let credentials = object as? [String: Any]
    else {
        return false
    }
    return credentials["claudeAiOauth"] is [String: Any]
}

private func isReviewRunDirectory(_ runURL: URL) throws -> Bool {
    var isDirectory: ObjCBool = false
    guard FileManager.default.fileExists(atPath: runURL.path, isDirectory: &isDirectory),
        isDirectory.boolValue
    else {
        return false
    }
    return !FileManager.default.fileExists(
        atPath: runURL.appendingPathComponent("attempt.json").path
    )
}

private func runGitDiffContext(
    arguments: [String],
    outputURL: URL,
    repoURL: URL,
    diffRange: String
) throws {
    try Data().write(to: outputURL, options: .atomic)
    let output = try FileHandle(forWritingTo: outputURL)
    defer { try? output.close() }

    let errorPipe = Pipe()
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
    process.arguments = ["git"] + arguments
    process.currentDirectoryURL = repoURL
    process.standardOutput = output
    process.standardError = errorPipe

    do {
        try process.run()
    } catch {
        try? FileManager.default.removeItem(at: outputURL)
        throw error
    }
    process.waitUntilExit()

    let errorData = errorPipe.fileHandleForReading.readDataToEndOfFile()
    let errorText = String(decoding: errorData, as: UTF8.self)

    guard process.terminationStatus == 0 else {
        try? FileManager.default.removeItem(at: outputURL)
        let command = (["git"] + arguments).joined(separator: " ")
        var message =
            "Could not resolve Git diff range `\(diffRange)` while generating Swift E2E review context."
        message += " Ensure the repository has enough history and the range is fetchable."
        message += " Command `\(command)` failed with exit status \(process.terminationStatus)."
        let detail = errorText.trimmingCharacters(in: .whitespacesAndNewlines)
        if !detail.isEmpty {
            message += "\n\(detail)"
        }
        throw ValidationError(message)
    }
}

private func runReviewDirectoryChildren(of url: URL) throws -> [URL] {
    guard FileManager.default.fileExists(atPath: url.path) else { return [] }
    return try FileManager.default.contentsOfDirectory(
        at: url,
        includingPropertiesForKeys: [.isDirectoryKey],
        options: [.skipsHiddenFiles]
    )
    .filter { (try? $0.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true }
    .sorted { $0.path < $1.path }
}

private func runReviewRelativeFilePath(
    fileName: String,
    attemptURL: URL,
    runURL: URL
) -> String? {
    let url = attemptURL.appendingPathComponent(fileName)
    guard FileManager.default.fileExists(atPath: url.path) else { return nil }
    return reviewRelativePath(url, base: runURL)
}

private func runReviewPrompt(
    basePrompt: String,
    repoURL: URL,
    packageURL: URL,
    testsURL: URL,
    runURL: URL,
    context: ReviewContext,
    overview: E2ERunOverview,
    reviewer: String,
    reviewDirectoryName: String,
    overwrite: Bool
) throws -> String {
    var lines = runPromptHeader(
        title: "Swift E2E run review",
        basePrompt: basePrompt,
        repoURL: repoURL,
        packageURL: packageURL,
        testsURL: testsURL,
        runURL: runURL,
        context: context,
        overviewURL: runOverviewURL(in: runURL)
    )
    lines.append("## Review scope")
    lines.append("")
    lines.append(
        "- Review directory: `\(runURL.appendingPathComponent(reviewDirectoryName).path)`"
    )
    appendReviewOutputContract(
        to: &lines,
        writableScopes: "report",
        reviewer: reviewer,
        reviewDirectoryName: reviewDirectoryName,
        overwrite: overwrite
    )
    lines.append("")
    appendRunOverviewReportFocus(overview, to: &lines)
    try appendRunReviewAttemptArtifacts(runURL: runURL, to: &lines)
    return lines.joined(separator: "\n")
}

private func runPromptHeader(
    title: String,
    basePrompt: String,
    repoURL: URL,
    packageURL: URL,
    testsURL: URL,
    runURL: URL,
    context: ReviewContext,
    overviewURL: URL
) -> [String] {
    var lines = [
        "# \(title)",
        "",
        basePrompt.trimmingCharacters(in: .whitespacesAndNewlines),
        "",
        "## Context",
        "",
        "- Repository root: `\(repoURL.path)`",
        "- Swift package: `\(packageURL.path)`",
        "- Swift E2E tests: `\(testsURL.path)`",
        "- Run directory: `\(runURL.path)`",
        "- Run overview JSON: `\(overviewURL.path)`",
        "",
        "Walk only the canonical run depth. Do not recursively scan copied `cli/` or `agent/` sandboxes unless you intentionally inspect a specific artifact referenced below.",
        "Print short `Progress:` lines while reviewing so CI shows activity.",
        "",
    ]

    appendReviewContext(context, to: &lines)
    return lines
}

private func appendReviewOutputContract(
    to lines: inout [String],
    writableScopes: String,
    reviewer: String,
    reviewDirectoryName: String,
    overwrite: Bool
) {
    lines.append("")
    lines.append("## Output contract")
    lines.append("")
    lines.append(
        "Write one Markdown file per actionable review issue under the top-level `\(reviewDirectoryName)/` directory. Writable scopes for this prompt: \(writableScopes)."
    )
    lines.append(
        "The file name must be the review title slug with `.md`: lowercase ASCII letters/digits, non-alphanumerics replaced by `-`, repeated dashes collapsed, and leading/trailing dashes removed. Example: `seed-cache-fixtures-before-listing.md`."
    )
    lines.append(
        "Use JSON `severity` to classify each issue as `info`, `concern`, or `fail`. Keep those exact JSON values. Do not include severity labels or severity emoji in review titles, Markdown headings, or summary text; the aggregate renderer adds the severity emoji from JSON. Do not use heart emojis as severity markers. Do not write prose status/severity lines such as `Status: pass`, `Status: concern`, or `Status: fail`."
    )
    lines.append(
        "If nothing is actionable, leave that `\(reviewDirectoryName)/` directory absent or empty."
    )
    if !overwrite {
        lines.append(
            "If existing review files are still valid and accurate, leave them in place; otherwise rewrite or remove stale files."
        )
    }
    lines.append("")
    lines.append("Each review file must have this exact shape:")
    lines.append("")
    lines.append("```md")
    lines.append("---")
    lines.append("{")
    lines.append("  \"schema\": \"\(e2eReviewSchemaID)\",")
    lines.append("  \"title\": \"Seed cache fixtures before listing values\",")
    lines.append("  \"scope\": \"report\",")
    lines.append("  \"reviewer\": \"\(reviewer)\",")
    lines.append("  \"severity\": \"concern\",")
    lines.append("  \"confidence\": \"medium\",")
    lines.append("  \"locations\": [")
    lines.append(
        "    { \"path\": \"swift/WendyE2ETests/Tests/WendyE2ETests/WendyCacheListTests.swift\", \"startLine\": 42, \"endLine\": 48 }"
    )
    lines.append("  ],")
    lines.append("  \"evidence\": [")
    lines.append(
        "    { \"path\": \"observations/wendy-cache-list/prints-values/ubuntu-24-04/0001/recording.md\" }"
    )
    lines.append("  ]")
    lines.append("}")
    lines.append("---")
    lines.append("")
    lines.append("# Seed cache fixtures before listing values")
    lines.append("")
    lines.append("Short GitHub-comment-sized summary of the finding and suggested action.")
    lines.append("")
    lines.append("## Details")
    lines.append("")
    lines.append("Full analysis, evidence, and suggested follow-up.")
    lines.append("```")
    lines.append("")
    lines.append(
        "The JSON `title` must match the Markdown `# Title`; the file name must be the slugged title; `scope` must be `report`; `reviewer` must be `\(reviewer)`."
    )
    lines.append(
        "Use `locations` only when the review is attributable to code lines in the repository. Use repo-relative paths and one-based line numbers. Use `evidence` for run-relative artifact paths."
    )
}

private func appendRunOverviewReportFocus(
    _ overview: E2ERunOverview,
    to lines: inout [String]
) {
    lines.append("## Run overview from `\(e2eRunOverviewFileName)`")
    lines.append("")
    lines.append(
        "The overview is generated before AI review from xUnit results and per-attempt artifacts. Use it as the source of truth for target-level deterministic failures and flakes."
    )
    lines.append("")
    lines.append(
        "- Summary: tests=\(overview.summary.tests), test-targets=\(overview.summary.testTargets), attempts=\(overview.summary.attemptResults), passed=\(overview.summary.passed), flaked=\(overview.summary.flaked), failed=\(overview.summary.failed), skipped=\(overview.summary.skipped), unknown=\(overview.summary.unknown), commands=\(overview.summary.commands)"
    )
    lines.append("- Target overview:")
    for target in overview.targets {
        lines.append(
            "  - `\(target.name)`: outcome=`\(target.outcome.rawValue)`, attempts=\(target.attempts), tests=\(target.tests), passed=\(target.passed), flaked=\(target.flaked), failed=\(target.failed), skipped=\(target.skipped), unknown=\(target.unknown)"
        )
    }
    lines.append("")

    appendRunOverviewIssues(
        title: "Deterministic failures",
        issues: overview.noteworthy.deterministicFailures,
        to: &lines
    )
    appendRunOverviewIssues(
        title: "Flakes",
        issues: overview.noteworthy.flakes,
        to: &lines
    )
    appendRunOverviewIssues(
        title: "Unknown outcomes",
        issues: overview.noteworthy.unknowns,
        to: &lines
    )
}

private func appendRunReviewAttemptArtifacts(runURL: URL, to lines: inout [String]) throws {
    let artifacts = try runReviewAttemptArtifacts(in: runURL)
    lines.append("## Attempt-level artifacts")
    lines.append("")
    lines.append(
        "Use these attempt-level artifacts when a job failed before Swift Testing produced per-test observations, or when `overview.json` is inconclusive. `attempt.log` is the full attempt setup/build/preflight/test-launch log captured by the runner."
    )
    lines.append("")

    guard !artifacts.isEmpty else {
        lines.append("- No attempt-level artifacts were recorded.")
        lines.append("")
        return
    }

    for artifact in artifacts {
        let exitStatus = artifact.exitStatus.map(String.init) ?? "unknown"
        let marker = artifact.needsDiagnosis ? "diagnosis-needed" : "ok"
        lines.append(
            "- target=`\(artifact.target)` attempt=`\(artifact.attempt)` exitStatus=`\(exitStatus)` observations=`\(artifact.observationCount)` marker=`\(marker)`"
        )
        if artifact.files.isEmpty {
            lines.append("  - files: `<none>`")
        } else {
            lines.append("  - files: \(artifact.files.map { "`\($0)`" }.joined(separator: ", "))")
        }
    }
    lines.append("")
}

private func runReviewAttemptArtifacts(in runURL: URL) throws -> [RunReviewAttemptArtifact] {
    var artifacts: [RunReviewAttemptArtifact] = []
    for targetURL in try runReviewDirectoryChildren(of: e2eAttemptArtifactsRootURL(in: runURL)) {
        let target = targetURL.lastPathComponent
        for attemptURL in try runReviewDirectoryChildren(of: targetURL) {
            let attempt = attemptURL.lastPathComponent
            artifacts.append(
                RunReviewAttemptArtifact(
                    target: target,
                    attempt: attempt,
                    exitStatus: runReviewAttemptExitStatus(attemptURL: attemptURL),
                    observationCount: try runReviewObservationCount(
                        runURL: runURL,
                        target: target,
                        attempt: attempt
                    ),
                    files: try runReviewAttemptFiles(attemptURL: attemptURL, runURL: runURL)
                )
            )
        }
    }
    return artifacts.sorted {
        if $0.target != $1.target { return $0.target < $1.target }
        return $0.attempt < $1.attempt
    }
}

private func runReviewAttemptExitStatus(attemptURL: URL) -> Int? {
    let url = attemptURL.appendingPathComponent("attempt.json")
    guard FileManager.default.fileExists(atPath: url.path),
        let data = try? Data(contentsOf: url),
        let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
    else {
        return nil
    }
    return object["exitStatus"] as? Int
}

private func runReviewAttemptFiles(attemptURL: URL, runURL: URL) throws -> [String] {
    let urls = try FileManager.default.contentsOfDirectory(
        at: attemptURL,
        includingPropertiesForKeys: [.isRegularFileKey],
        options: [.skipsHiddenFiles]
    )
    return urls
        .filter { (try? $0.resourceValues(forKeys: [.isRegularFileKey]).isRegularFile) == true }
        .sorted { $0.lastPathComponent < $1.lastPathComponent }
        .map { reviewRelativePath($0, base: runURL) }
}

private func runReviewObservationCount(runURL: URL, target: String, attempt: String) throws -> Int {
    var count = 0
    for suiteURL in try runReviewDirectoryChildren(of: e2eObservationsRootURL(in: runURL)) {
        for testURL in try runReviewDirectoryChildren(of: suiteURL) {
            let observationURL = testURL
                .appendingPathComponent(target, isDirectory: true)
                .appendingPathComponent(attempt, isDirectory: true)
            var isDirectory: ObjCBool = false
            if FileManager.default.fileExists(atPath: observationURL.path, isDirectory: &isDirectory),
                isDirectory.boolValue
            {
                count += 1
            }
        }
    }
    return count
}

private func appendRunOverviewIssues(
    title: String,
    issues: [E2ERunOverviewIssue],
    to lines: inout [String]
) {
    lines.append("### \(title)")
    lines.append("")
    guard !issues.isEmpty else {
        lines.append("- None recorded.")
        lines.append("")
        return
    }

    for issue in issues {
        lines.append(
            "- `\(issue.suite)/\(issue.test)` target=`\(issue.target)` outcome=`\(issue.outcome.rawValue)` attempts=\(runOverviewIssueAttemptSummary(issue.attempts))"
        )
        for attempt in issue.attempts where attempt.status != .passed || issue.outcome == .flaked {
            appendRunOverviewIssueAttempt(attempt, prefix: "  ", to: &lines)
        }
    }
    lines.append("")
}

private func appendRunOverviewIssueAttempt(
    _ attempt: E2ERunOverviewIssueAttempt,
    prefix: String,
    to lines: inout [String]
) {
    let duration = attempt.durationSeconds.map(formatSeconds) ?? "unknown"
    lines.append(
        "\(prefix)- attempt=`\(attempt.attempt)` status=`\(attempt.status.rawValue)` duration=`\(duration)`"
    )
    appendRunOverviewDetail(attempt.detail, prefix: "\(prefix)  ", to: &lines)
    appendRunOverviewArtifacts(attempt.artifacts, prefix: "\(prefix)  ", to: &lines)
}

private func appendRunOverviewDetail(
    _ detail: String?,
    prefix: String,
    to lines: inout [String]
) {
    guard let detail, !detail.isEmpty else { return }
    lines.append("\(prefix)detail: \(runOverviewSingleLine(detail, limit: 500))")
}

private func appendRunOverviewArtifacts(
    _ artifacts: E2ERunOverviewArtifacts,
    prefix: String,
    to lines: inout [String]
) {
    if let recording = artifacts.recording {
        lines.append("\(prefix)recording: `\(recording)`")
    }
    if let shell = artifacts.shell {
        lines.append("\(prefix)shell: `\(shell)`")
    }
    if let testResults = artifacts.testResults {
        lines.append("\(prefix)xunit: `\(testResults)`")
    }
}

private func runOverviewIssueAttemptSummary(_ attempts: [E2ERunOverviewIssueAttempt]) -> String {
    attempts.map { "\($0.attempt):\($0.status.rawValue)" }.joined(separator: ",")
}

private func runOverviewSingleLine(_ value: String, limit: Int) -> String {
    let singleLine = value.replacingOccurrences(of: "\n", with: " ")
        .trimmingCharacters(in: .whitespacesAndNewlines)
    guard singleLine.count > limit else { return singleLine }
    return String(singleLine.prefix(limit)) + "…"
}

private func appendReviewContext(_ context: ReviewContext, to lines: inout [String]) {
    lines.append("## Review mode")
    lines.append("")
    lines.append("- Mode: `\(context.mode.name)`")

    guard case .diff(let range) = context.mode else {
        lines.append("")
        return
    }

    lines.append("- Git diff range: `\(range)`")
    if let changedFilesURL = context.changedFilesURL {
        lines.append("- Changed files: `\(changedFilesURL.path)`")
    }
    if let diffstatURL = context.diffstatURL {
        lines.append("- Diffstat: `\(diffstatURL.path)`")
    }
    lines.append("")
    lines.append(
        "Only report findings plausibly related to the supplied Git diff range. Treat unrelated pre-existing failures, flakes, or test quality issues as background unless the diff appears to introduce or worsen them."
    )
    lines.append(
        "Do not look for a saved full patch; inspect targeted diffs on demand with commands like:"
    )
    lines.append("")
    lines.append("```bash")
    lines.append("git diff --stat \(range)")
    lines.append("git diff --name-only \(range)")
    lines.append("git diff \(range) -- <specific-file>")
    lines.append("git diff \(range) -U80 -- <specific-file>")
    lines.append("```")
    lines.append("")
}

private func removeExistingRunReviews(in runURL: URL, reviewer: String) throws {
    removeRunReviews(in: runURL, reviewer: reviewer)
    for suiteURL in try runReviewDirectoryChildren(of: e2eObservationsRootURL(in: runURL)) {
        guard !isE2EReviewDirectoryName(suiteURL.lastPathComponent) else { continue }
        removeRunReviews(in: suiteURL, reviewer: reviewer)
        for testURL in try runReviewDirectoryChildren(of: suiteURL) {
            removeRunReviews(in: testURL, reviewer: reviewer)
        }
    }
}

private func removeRunReviews(in directoryURL: URL, reviewer: String) {
    try? FileManager.default.removeItem(
        at: directoryURL.appendingPathComponent(e2eReviewDirectoryName(reviewer: reviewer))
    )
}

private func enforceRunReportReviewContract(in runURL: URL, reviewer: String) throws {
    _ = try enforceReviews(
        in: runURL,
        expectedScope: "report",
        expectedReviewer: reviewer,
        runURL: runURL
    )
}

@discardableResult
private func enforceReviews(
    in directoryURL: URL,
    expectedScope: String,
    expectedReviewer: String,
    runURL: URL
) throws -> Int {
    let reviewURL = directoryURL.appendingPathComponent(
        e2eReviewDirectoryName(reviewer: expectedReviewer)
    )
    guard FileManager.default.fileExists(atPath: reviewURL.path) else {
        return 0
    }
    let reviewURLs = try FileManager.default.contentsOfDirectory(
        at: reviewURL,
        includingPropertiesForKeys: [.isRegularFileKey],
        options: [.skipsHiddenFiles]
    )
    .filter { $0.pathExtension.lowercased() == "md" }

    var count = 0
    for reviewURL in reviewURLs {
        do {
            _ = try parseE2EReview(
                at: reviewURL,
                expectedScope: expectedScope,
                expectedReviewer: expectedReviewer,
                relativeTo: runURL
            )
            count += 1
        } catch {
            try? FileManager.default.removeItem(at: reviewURL)
        }
    }
    return count
}

private func defaultReviewTestsDir(packageURL: URL) -> URL {
    let e2eTestsURL = packageURL.appendingPathComponent("Tests/WendyE2ETests")
    if FileManager.default.fileExists(atPath: e2eTestsURL.path) {
        return e2eTestsURL
    }
    return packageURL.appendingPathComponent("Tests")
}

private func reviewRelativePath(_ url: URL, base: URL) -> String {
    let path = url.path
    let basePath = base.path
    if path.hasPrefix(basePath + "/") {
        return String(path.dropFirst(basePath.count + 1))
    }
    if let range = path.range(of: "/tests/") {
        return "tests/" + path[range.upperBound...]
    }
    return path
}

private func formatSeconds(_ value: Double) -> String {
    if value.rounded() == value {
        return String(Int(value))
    }
    return String(format: "%.3f", value)
}
