package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RootDetectorTest {
    @Test
    fun detectsTestKeysWithoutFileScanHit() {
        assertTrue(
            RootDetector.isLikelyRooted(
                buildTags = "release-keys test-keys",
                fileExists = { false },
            ),
        )
    }

    @Test
    fun detectsKnownRootMarkerPath() {
        assertTrue(
            RootDetector.isLikelyRooted(
                buildTags = "release-keys",
                fileExists = { it == "/system/xbin/su" },
            ),
        )
    }

    @Test
    fun ignoresMissingMarkersAndFileErrors() {
        assertFalse(
            RootDetector.isLikelyRooted(
                buildTags = "release-keys",
                fileExists = { throw SecurityException("blocked") },
            ),
        )
    }
}
