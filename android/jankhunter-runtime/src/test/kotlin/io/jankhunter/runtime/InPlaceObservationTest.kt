package io.jankhunter.runtime

import java.io.File
import java.nio.file.Files

class InPlaceObservationTest : InPlaceObservationContract() {
    override fun testDirectory(): File = Files.createTempDirectory("jh-in-place").toFile()
}
