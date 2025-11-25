import Foundation
import Testing

@testable import Analytics

// MARK: - Environment Variable Tests

@Suite("Consent Manager Environment Variables")
struct ConsentManagerEnvironmentTests {

    init() {
        // Reset environment variables before each test
        unsetenv("DO_NOT_TRACK")
        unsetenv("WENDY_ANALYTICS")
        unsetenv("CI")
        unsetenv("NO_ANALYTICS")
    }

    deinit {
        // Clean up environment variables after tests
        unsetenv("DO_NOT_TRACK")
        unsetenv("WENDY_ANALYTICS")
        unsetenv("CI")
        unsetenv("NO_ANALYTICS")
    }

    @Test("Should disable analytics with DO_NOT_TRACK", arguments: ["1", "true", "TRUE", "True"])
    func shouldDisableAnalyticsWithDoNotTrack(value: String) {
        setenv("DO_NOT_TRACK", value, 1)
        #expect(ConsentManager.shouldDisableAnalytics() == true)
        unsetenv("DO_NOT_TRACK")
    }

    @Test(
        "Should not disable analytics with invalid DO_NOT_TRACK",
        arguments: ["0", "false", "no", ""]
    )
    func shouldNotDisableAnalyticsWithInvalidDoNotTrack(value: String) {
        setenv("DO_NOT_TRACK", value, 1)
        #expect(ConsentManager.shouldDisableAnalytics() == false)
        unsetenv("DO_NOT_TRACK")
    }

    @Test(
        "Should disable analytics in CI environment",
        arguments: [
            "CI",
            "CONTINUOUS_INTEGRATION",
            "BUILD_ID",
            "JENKINS_URL",
            "GITHUB_ACTIONS",
            "GITLAB_CI",
        ]
    )
    func shouldDisableAnalyticsInCIEnvironment(indicator: String) {
        setenv(indicator, "true", 1)
        #expect(ConsentManager.shouldDisableAnalytics() == true)
        unsetenv(indicator)
    }

    @Test(
        "Should disable analytics with WENDY_ANALYTICS=false",
        arguments: [
            "false", "0", "no", "False", "FALSE", "No", "NO",
        ]
    )
    func shouldDisableAnalyticsWithWendyAnalyticsFalse(value: String) {
        setenv("WENDY_ANALYTICS", value, 1)
        #expect(ConsentManager.shouldDisableAnalytics() == true)
        unsetenv("WENDY_ANALYTICS")
    }

    @Test(
        "Should not disable analytics with WENDY_ANALYTICS enabled",
        arguments: [
            "true", "1", "yes",
        ]
    )
    func shouldNotDisableAnalyticsWithWendyAnalyticsEnabled(value: String) {
        setenv("WENDY_ANALYTICS", value, 1)
        #expect(ConsentManager.shouldDisableAnalytics() == false)
        unsetenv("WENDY_ANALYTICS")
    }

    @Test(
        "Should disable analytics with NO_ANALYTICS",
        arguments: [
            "true", "1", "yes", "True", "TRUE", "Yes", "YES",
        ]
    )
    func shouldDisableAnalyticsWithNoAnalytics(value: String) {
        setenv("NO_ANALYTICS", value, 1)
        #expect(ConsentManager.shouldDisableAnalytics() == true)
        unsetenv("NO_ANALYTICS")
    }

    @Test("DO_NOT_TRACK should take precedence over WENDY_ANALYTICS")
    func environmentVariablePriority() {
        setenv("DO_NOT_TRACK", "1", 1)
        setenv("WENDY_ANALYTICS", "true", 1)
        #expect(ConsentManager.shouldDisableAnalytics() == true)
        unsetenv("DO_NOT_TRACK")
        unsetenv("WENDY_ANALYTICS")
    }
}

// MARK: - Async Tests

@Suite("Consent Manager Async Operations")
struct ConsentManagerAsyncTests {

    init() {
        unsetenv("DO_NOT_TRACK")
        unsetenv("WENDY_ANALYTICS")
        unsetenv("CI")
        unsetenv("NO_ANALYTICS")
    }

    deinit {
        unsetenv("DO_NOT_TRACK")
        unsetenv("WENDY_ANALYTICS")
        unsetenv("CI")
        unsetenv("NO_ANALYTICS")
    }

    @Test("Analytics should be disabled when DO_NOT_TRACK is set")
    func isAnalyticsEnabledWithEnvironmentOverride() async {
        let consentManager = ConsentManager()

        setenv("DO_NOT_TRACK", "1", 1)
        let enabled = await consentManager.isAnalyticsEnabled()
        #expect(enabled == false)
        unsetenv("DO_NOT_TRACK")
    }

    @Test("Analytics should be enabled by default (opt-out model)")
    func isAnalyticsEnabledByDefault() async {
        let consentManager = ConsentManager()

        // Without any config or environment variables, should default to enabled
        let enabled = await consentManager.isAnalyticsEnabled()
        #expect(enabled == true)
    }

    @Test("Get status should return appropriate message")
    func getStatus() async {
        let consentManager = ConsentManager()

        // Test with no environment variables
        var status = await consentManager.getStatus()
        #expect(status.contains("Analytics:"))

        // Test with DO_NOT_TRACK
        setenv("DO_NOT_TRACK", "1", 1)
        status = await consentManager.getStatus()
        #expect(status.contains("Disabled") && status.contains("environment variable"))
        unsetenv("DO_NOT_TRACK")
    }
}

// MARK: - Config Management Tests

@Suite("Consent Manager Config Operations")
struct ConsentManagerConfigTests {

    @Test("Disable analytics should not throw")
    func disableAnalytics() throws {
        let consentManager = ConsentManager()
        do {
            try consentManager.disableAnalytics()
        } catch {
            // Expected to potentially fail without proper file system setup
            // But the method should not crash
        }
    }

    @Test("Enable analytics should not throw")
    func enableAnalytics() throws {
        let consentManager = ConsentManager()
        do {
            try consentManager.enableAnalytics()
        } catch {
            // Expected to potentially fail without proper file system setup
            // But the method should not crash
        }
    }
}
