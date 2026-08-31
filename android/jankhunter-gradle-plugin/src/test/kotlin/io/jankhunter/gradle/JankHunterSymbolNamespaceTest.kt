package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterSymbolNamespaceTest {
    @Test
    fun applicationAndLibraryModulesShareTheGlobalContractNamespace() {
        val applicationNamespace = JankHunterSymbolNamespace.current()
        val libraryNamespace = JankHunterSymbolNamespace.generate(
            JankHunterSymbolNamespace.currentContract(),
        )

        assertEquals(applicationNamespace, libraryNamespace)
        assertEquals("883f693e017ddfa095fd73c4126020db", applicationNamespace)
        assertTrue(applicationNamespace.matches(Regex("[0-9a-f]{32}")))
    }

    @Test
    fun incompatibleSchemaOrStableIdAlgorithmChangesTheNamespace() {
        val contract = JankHunterSymbolNamespace.currentContract()
        val namespace = JankHunterSymbolNamespace.generate(contract)

        assertNotEquals(
            namespace,
            JankHunterSymbolNamespace.generate(
                contract.copy(embeddedSymbolFormat = contract.embeddedSymbolFormat + 1),
            ),
        )
        assertNotEquals(
            namespace,
            JankHunterSymbolNamespace.generate(
                contract.copy(stableIdAlgorithm = contract.stableIdAlgorithm + ";v=2"),
            ),
        )
        assertNotEquals(
            namespace,
            JankHunterSymbolNamespace.generate(
                contract.copy(stableIdEncoding = contract.stableIdEncoding + ";v=2"),
            ),
        )
    }
}
