package io.jankhunter.runtime

import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID

class RuntimeEnabledStateArtTest : RuntimeEnabledStateContract() {
    private val root = File(testContext().cacheDir, "runtime-enabled-${UUID.randomUUID()}")
    override fun testDirectory(): File = File(root, "logs")
    override fun testContext(): Context = InstrumentationRegistry.getInstrumentation().targetContext
}
