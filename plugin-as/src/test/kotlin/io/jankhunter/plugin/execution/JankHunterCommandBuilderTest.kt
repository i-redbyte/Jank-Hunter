package io.jankhunter.plugin.execution

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertThrows

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
            val old = log(dir, sessionName("2026-07-14", RUN_1, 9), modifiedAt = 3_000)
            val latest = log(dir, sessionName("2026-07-14", RUN_2, 10), modifiedAt = 1_000)

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
            val old = log(dir, sessionName("2026-07-13", RUN_1, 99), modifiedAt = 3_000)
            val latest = log(dir, sessionName("2026-07-14", RUN_2, 0), modifiedAt = 1_000)

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
            val canonical = log(dir, sessionName("2026-07-14", RUN_1, 0), modifiedAt = 1_000)
            val nonCanonical = log(dir, "jh-session-log.2026-07-15.$RUN_2.01.jhlog", modifiedAt = 3_000)

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

    fun testInspectLatestLogScopeRejectsDirectoryWithoutCanonicalLogs() {
        withTempLogDir { dir ->
            log(dir, "jh-session-log.2026-07-15.1.jhlog", modifiedAt = 3_000)

            assertThrows(IllegalArgumentException::class.java) {
                JankHunterCommandBuilder.build(
                    project,
                    request(
                        logs = "${dir.path}/*.jhlog",
                        inspectLogScope = JankHunterLogScope.LATEST_LOG,
                    ),
                )
            }
        }
    }

    fun testInspectLatestLogScopeKeepsEverySegmentAndProcessFromLatestRun() {
        withTempLogDir { dir ->
            val old = log(dir, sessionName("2026-07-14", RUN_1, 8), modifiedAt = 4_000)
            val main = log(dir, sessionName("2026-07-14", RUN_2, 9), modifiedAt = 1_000)
            val remote = log(dir, sessionName("2026-07-14", RUN_2, 10), modifiedAt = 2_000)

            val command = JankHunterCommandBuilder.build(
                project,
                request(
                    logs = "${dir.path}/*.jhlog",
                    inspectLogScope = JankHunterLogScope.LATEST_LOG,
                ),
            )

            assertFalse(command.args.contains(old.toPath().normalize().toString()))
            assertTrue(command.args.contains(main.toPath().normalize().toString()))
            assertTrue(command.args.contains(remote.toPath().normalize().toString()))
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

    fun testInspectForwardsArtifactDirectory() {
        val command = JankHunterCommandBuilder.build(
            project,
            request(logs = "/tmp/run.jhlog").copy(artifactsDir = "/tmp/generated/jankhunter/debug"),
        )

        val flagIndex = command.args.indexOf("--artifacts-dir")
        assertTrue(flagIndex >= 0)
        assertEquals("/tmp/generated/jankhunter/debug", command.args[flagIndex + 1])
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

    fun testInspectForwardsCurrentAppearanceOptions() {
        val command = JankHunterCommandBuilder.build(
            project,
            request(logs = "/tmp/run.jhlog").copy(
                presentation = true,
                animatedBackground = true,
            ),
        )

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
            artifactsDir = "",
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
            animatedBackground = false,
    )

    private fun sessionName(date: String, runId: String, index: Long): String =
        "jh-session-log.$date.$runId.$index.jhlog"

    private companion object {
        const val RUN_1 = "01000000000000000000000000000000"
        const val RUN_2 = "02000000000000000000000000000000"
    }

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
