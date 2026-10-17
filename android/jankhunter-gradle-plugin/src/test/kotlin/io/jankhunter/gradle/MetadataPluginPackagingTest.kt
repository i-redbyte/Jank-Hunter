package io.jankhunter.gradle

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MetadataPluginPackagingTest {
    @Test
    fun readerPackIsBundledAsPrivatePluginResource() {
        val capabilities = MetadataReaderCapabilities::class.java.getResource(
            MetadataReaderCapabilities.RESOURCE_PREFIX + "capabilities.properties",
        )
        assertNotNull(capabilities)
        val index = MetadataReaderCapabilities::class.java.getResource(
            MetadataReaderCapabilities.RESOURCE_PREFIX + "index",
        )
        assertNotNull(index)
        assertTrue(MetadataReaderCapabilities.identity.isNotBlank())
    }

    @Test
    fun readerBackendJarsAreListedAsPrivatePluginResources() {
        val index = checkNotNull(
            MetadataReaderCapabilities::class.java.getResourceAsStream(
                MetadataReaderCapabilities.RESOURCE_PREFIX + "index",
            ),
        ).bufferedReader().readText()
        assertTrue(index.lines().any { it.endsWith(".jar") })
    }
}
