package io.jankhunter.runtime

import java.io.File
import java.nio.file.Files

class IntegrationBoundarySafetyTest : IntegrationBoundarySafetyContract() {
    override fun newDirectory(name: String): File = Files.createTempDirectory(name).toFile()
}
