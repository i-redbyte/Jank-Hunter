package io.jankhunter.plugin.execution

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.io.File
import java.nio.file.Files

class JankHunterCommandBuilderTest : BasePlatformTestCase() {
    fun testInspectDefaultsToAllSessions() {
        val command = JankHunterCommandBuilder.build(
            project,
            request(logs = "/tmp/first.jhlog, /tmp/second.jhlog"),
        )

        assertTrue(command.args.contains("--all-sessions"))
    }

    fun testInspectLatestLogScopeUsesCanonicalNumericIndex() {
        withTempLogDir { dir ->
            val old = log(dir, "jh-session-log.2026-07-14.9.jhlog", modifiedAt = 3_000)
            val latest = log(dir, "jh-session-log.2026-07-14.10.jhlog", modifiedAt = 1_000)

            val command = JankHunterCommandBuilder.build(
                project,
                request(
                    logs = "${dir.path}/*.jhlog",
                    inspectLogScope = JankHunterLogScope.LATEST_LOG,
                ),
            )

            assertFalse(command.args.contains("--all-sessions"))
            assertTrue(command.args.contains(latest.toPath().normalize().toString()))
            assertFalse(command.args.contains(old.toPath().normalize().toString()))
        }
    }

    fun testInspectLatestLogScopeUsesNewestCanonicalDateBeforeIndex() {
        withTempLogDir { dir ->
            val old = log(dir, "jh-session-log.2026-07-13.99.jhlog", modifiedAt = 3_000)
            val latest = log(dir, "jh-session-log.2026-07-14.0.jhlog", modifiedAt = 1_000)

            val command = JankHunterCommandBuilder.build(
                project,
                request(
                    logs = "${dir.path}/*.jhlog",
                    inspectLogScope = JankHunterLogScope.LATEST_LOG,
                ),
            )

            assertFalse(command.args.contains("--all-sessions"))
            assertFalse(command.args.contains(old.toPath().normalize().toString()))
            assertTrue(command.args.contains(latest.toPath().normalize().toString()))
        }
    }

    fun testInspectLatestLogScopeRejectsNonCanonicalLeadingZeroIndex() {
        withTempLogDir { dir ->
            val canonical = log(dir, "jh-session-log.2026-07-14.0.jhlog", modifiedAt = 1_000)
            val nonCanonical = log(dir, "jh-session-log.2026-07-15.01.jhlog", modifiedAt = 3_000)

            val command = JankHunterCommandBuilder.build(
                project,
                request(
                    logs = "${dir.path}/*.jhlog",
                    inspectLogScope = JankHunterLogScope.LATEST_LOG,
                ),
            )

            assertTrue(command.args.contains(canonical.toPath().normalize().toString()))
            assertFalse(command.args.contains(nonCanonical.toPath().normalize().toString()))
        }
    }

    fun testInspectForwardsDependencyInjectionCatalog() {
        val command = JankHunterCommandBuilder.build(
            project,
            request(logs = "/tmp/run.jhlog", diCatalog = "/tmp/di-catalog.jsonl"),
        )

        val flagIndex = command.args.indexOf("--di-catalog")
        assertTrue(flagIndex >= 0)
        assertEquals("/tmp/di-catalog.jsonl", command.args[flagIndex + 1])
    }

    fun testProblemsExportRemainsAvailableOutsideTheMainUi() {
        val command = JankHunterCommandBuilder.build(
            project,
            request(logs = "/tmp/run.jhlog").copy(
                mode = JankHunterMode.PROBLEMS,
                output = "/tmp/problems.csv",
                dataset = "code-problems",
                format = "csv",
            ),
        )

        assertEquals("problems", command.args.first())
        assertTrue(command.args.containsAll(listOf("--dataset", "code-problems")))
        assertTrue(command.args.containsAll(listOf("--format", "csv")))
        assertEquals("/tmp/problems.csv", command.outputPath)
    }

    fun testInspectForwardsAdvancedAppearanceOptions() {
        val command = JankHunterCommandBuilder.build(
            project,
            request(logs = "/tmp/run.jhlog").copy(
                reportStyle = "legacy",
                presentation = true,
                animatedBackground = true,
            ),
        )

        assertTrue(command.args.containsAll(listOf("--report-style", "legacy")))
        assertTrue(command.args.contains("--presentation"))
        assertTrue(command.args.contains("--animated-background"))
    }

    private fun request(
        logs: String,
        inspectLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED,
        diCatalog: String = "",
    ): JankHunterRunRequest =
        JankHunterRunRequest(
            mode = JankHunterMode.INSPECT,
            cliPath = "jankhunter",
            logs = logs,
            inspectLogScope = inspectLogScope,
            baseline = "",
            candidate = "",
            output = "/tmp/report.html",
            ownerMap = "",
            mapping = "",
            classGraph = "",
            diagnostics = "",
            diCatalog = diCatalog,
            heapDump = "",
            heapEvidence = "",
            baselineHeapDump = "",
            baselineHeapEvidence = "",
            candidateHeapDump = "",
            candidateHeapEvidence = "",
            route = "",
            screen = "",
            owner = "",
            className = "",
            dataset = "",
            format = "",
            json = false,
            presentation = false,
            reportStyle = "modern",
            animatedBackground = false,
        )

    private fun withTempLogDir(block: (File) -> Unit) {
        val dir = Files.createTempDirectory("jankhunter-plugin-test").toFile()
        try {
            block(dir)
        } finally {
            dir.deleteRecursively()
        }
    }

    private fun log(dir: File, name: String, modifiedAt: Long): File =
        File(dir, name).apply {
            writeText("{}\n")
            setLastModified(modifiedAt)
        }
}
