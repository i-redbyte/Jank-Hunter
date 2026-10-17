package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID

class IntegrationBoundarySafetyArtTest : IntegrationBoundarySafetyContract() {
    override fun newDirectory(name: String): File = File(
        InstrumentationRegistry.getInstrumentation().targetContext.cacheDir, "$name-${UUID.randomUUID()}",
    ).apply { mkdirs() }
}
