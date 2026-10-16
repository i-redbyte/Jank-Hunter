package io.jankhunter.runtime.internal.io

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ArtifactRootContainmentTest {
    @Test
    fun comparisonUsesPathComponentsIncludingFilesystemRoot() {
        val root = File("/artifacts").canonicalFile
        assertTrue(isWithinArtifactRoot(root, root))
        assertTrue(isWithinArtifactRoot(File(root, "session/file"), root))
        assertFalse(isWithinArtifactRoot(File("/artifacts-other"), root))
        assertFalse(isWithinArtifactRoot(File("/artifact"), root))
        assertTrue(isWithinArtifactRoot(root, File("/")))
    }
}
