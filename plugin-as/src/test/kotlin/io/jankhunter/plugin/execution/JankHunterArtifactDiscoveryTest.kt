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
        generated.resolve("di-catalog.jsonl").writeText("{}\n")
        val mapping = root.resolve("sample-app/build/outputs/mapping/debug")
        mapping.mkdirs()
        mapping.resolve("mapping.txt").writeText("")

        val sets = JankHunterArtifactDiscovery.findArtifactSets(root)

        assertEquals(1, sets.size)
        assertEquals("sample-app:debug", sets[0].variant)
        assertTrue(sets[0].ownerMap.endsWith("owner-map.json"))
        assertTrue(sets[0].mapping.endsWith("mapping.txt"))
        assertTrue(sets[0].diCatalog.endsWith("di-catalog.jsonl"))
    }

    @Test
    fun keepsSameVariantArtifactsSeparatedByModule() {
        val root = Files.createTempDirectory("jankhunter-discovery-modules").toFile()
        val app = root.resolve("app/build/generated/jankhunter/debug").apply { mkdirs() }
        app.resolve("owner-map.json").writeText("{}")
        val feature = root.resolve("feature/feed/build/generated/jankhunter/debug").apply { mkdirs() }
        feature.resolve("di-catalog.jsonl").writeText("{}\n")

        val sets = JankHunterArtifactDiscovery.findArtifactSets(root).associateBy { it.variant }

        assertEquals(2, sets.size)
        assertTrue(sets.getValue("app:debug").ownerMap.endsWith("owner-map.json"))
        assertEquals("", sets.getValue("app:debug").diCatalog)
        assertTrue(sets.getValue("feature/feed:debug").diCatalog.endsWith("di-catalog.jsonl"))
        assertEquals("", sets.getValue("feature/feed:debug").ownerMap)
    }
}
