package io.jankhunter.plugin.execution

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class JankHunterArtifactDiscoveryTest {
    @Test
    fun groupsArtifactsByVariant() {
        val root = Files.createTempDirectory("jankhunter-discovery").toFile()
        val generated = root.resolve("sample-app/build/generated/jankhunter/debug")
        generated.mkdirs()
        generated.resolve("owner-map.json").writeText("{}")
        generated.resolve("class-graph.jsonl").writeText("{}\n")
        generated.resolve("instrumentation-diagnostics.jsonl").writeText("{}\n")
        val mapping = root.resolve("sample-app/build/outputs/mapping/debug")
        mapping.mkdirs()
        mapping.resolve("mapping.txt").writeText("")

        val sets = JankHunterArtifactDiscovery.findArtifactSets(root)

        assertEquals(1, sets.size)
        assertEquals("debug", sets[0].variant)
        assertTrue(sets[0].ownerMap.endsWith("owner-map.json"))
        assertTrue(sets[0].mapping.endsWith("mapping.txt"))
    }
}
