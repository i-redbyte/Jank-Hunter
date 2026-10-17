package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import java.io.File
import java.nio.file.Files

class RuntimeEnabledStateTest : RuntimeEnabledStateContract() {
    private val root = Files.createTempDirectory("runtime-enabled-state").toFile()
    override fun testDirectory(): File = File(root, "logs")
    override fun testContext(): Context = object : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this
        override fun getPackageName(): String = "com.example"
        override fun getFilesDir(): File = File(root, "files").apply { mkdirs() }
        override fun getSystemService(name: String): Any? = null
    }
}
