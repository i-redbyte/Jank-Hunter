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

    fun testInspectLatestLogScopeUsesNewestExpandedFile() {
        withTempLogDir { dir ->
            val old = log(dir, "session-app-1000-0.jhlog", modifiedAt = 1_000)
            val latest = log(dir, "session-app-2000-0.jhlog", modifiedAt = 2_000)

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

    fun testLatestSessionGroupKeepsNewestParts() {
        withTempLogDir { dir ->
            val old = log(dir, "session-app-1000-0.jhlog", modifiedAt = 1_000)
            val latestPart0 = log(dir, "session-app-2000-0.jhlog", modifiedAt = 2_000)
            val latestPart1 = log(dir, "session-app-2000-1.jhlog", modifiedAt = 2_001)

            val command = JankHunterCommandBuilder.build(
                project,
                request(
                    logs = "${dir.path}/*.jhlog",
                    inspectLogScope = JankHunterLogScope.LATEST_SESSION_GROUP,
                ),
            )

            assertFalse(command.args.contains("--all-sessions"))
            assertFalse(command.args.contains(old.toPath().normalize().toString()))
            assertTrue(command.args.contains(latestPart0.toPath().normalize().toString()))
            assertTrue(command.args.contains(latestPart1.toPath().normalize().toString()))
        }
    }

    private fun request(
        logs: String,
        inspectLogScope: JankHunterLogScope = JankHunterLogScope.ALL_SELECTED,
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
