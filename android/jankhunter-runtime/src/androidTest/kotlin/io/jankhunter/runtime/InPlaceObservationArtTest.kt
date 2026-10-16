package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID

class InPlaceObservationArtTest : InPlaceObservationContract() {
    override fun testDirectory(): File = File(
        InstrumentationRegistry.getInstrumentation().targetContext.cacheDir, "in-place-${UUID.randomUUID()}")
}
