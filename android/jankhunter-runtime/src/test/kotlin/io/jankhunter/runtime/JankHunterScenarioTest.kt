package io.jankhunter.runtime

import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterScenarioTest {
    @After
    fun tearDown() {
        JankHunter.shutdown()
    }

    @Test
    fun definitionPrependsReservedIdentityAndKeepsSixConditions() {
        val conditions = JankHunterOperationAttributes.fromEntries(
            "account", "existing",
            "cache", "warm",
            "network", "wifi",
            "payload", "small",
            "source", "push",
            "experiment", "control",
        )

        val scenario = JankHunterScenario.create("chat.open", "2", conditions)

        assertEquals(8, scenario.operationAttributes.size)
        assertEquals("jh.scenario", scenario.operationAttributes.key(0))
        assertEquals("chat.open", scenario.operationAttributes.value(0))
        assertEquals("jh.scenario_rev", scenario.operationAttributes.key(1))
        assertEquals("2", scenario.operationAttributes.value(1))
        assertEquals("experiment", scenario.operationAttributes.key(7))
    }

    @Test
    fun definitionRejectsBlankUnknownReservedAndExcessConditions() {
        val invalid = listOf(
            JankHunterOperationAttributes.of("jh.scenario", "other"),
            JankHunterOperationAttributes.of("jh.scenario_rev", "other"),
            JankHunterOperationAttributes.fromEntries(
                "a", "1", "b", "2", "c", "3", "d", "4", "e", "5", "f", "6", "g", "7",
            ),
        )
        invalid.forEach { conditions ->
            assertFails { JankHunterScenario.create("chat.open", "1", conditions) }
        }
        for (value in listOf("", " ", "unknown")) {
            assertFails { JankHunterScenario.create(value, "1") }
            assertFails { JankHunterScenario.create("chat.open", value) }
        }
    }

    @Test
    fun definitionUsesUtf8ByteLimitForIdentityAndConditionValues() {
        val boundary = "я".repeat(64)
        JankHunterScenario.create(boundary, boundary, JankHunterOperationAttributes.of("payload", boundary))

        val overflow = "я".repeat(65)
        assertFails { JankHunterScenario.create(overflow, "1") }
        assertFails { JankHunterScenario.create("chat.open", overflow) }
        assertFails {
            JankHunterScenario.create("chat.open", "1", JankHunterOperationAttributes.of("payload", overflow))
        }
    }

    @Test
    fun disabledRuntimeReturnsExistingNoopForRootAndStage() {
        val scenario = JankHunterScenario.create("chat.open", "1")

        val root = scenario.start("chat.open")
        val stage = scenario.startStage("load.messages")

        assertSame(JankHunterOperation.NONE, root)
        assertSame(JankHunterOperation.NONE, stage)
        assertEquals(JankHunterOperationKind.SYSTEM, root.kind)
        assertTrue(root.isFinished)
    }

    @Test
    fun tracePreservesSuccessFailureAndCancellationApis() {
        val scenario = JankHunterScenario.create("chat.open", "1")
        assertEquals("done", scenario.trace("chat.open") { "done" })

        val failure = runCatching { scenario.trace<Unit>("chat.open") { error("boom") } }
        assertTrue(failure.isFailure)

        val operation = scenario.start("chat.open")
        assertTrue(operation.isFinished)
        assertTrue(!operation.cancel())
    }

    private fun assertFails(block: () -> Unit) {
        assertTrue(runCatching(block).isFailure)
    }
}
