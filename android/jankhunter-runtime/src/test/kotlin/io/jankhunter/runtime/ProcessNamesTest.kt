package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ProcessNames
import org.junit.Assert.assertEquals
import org.junit.Test

class ProcessNamesTest {
    @Test
    fun safeFileSuffixUsesMainForPackageProcess() {
        assertEquals("main", ProcessNames.safeFileSuffix("com.example.app", "com.example.app"))
    }

    @Test
    fun safeFileSuffixUsesShortColonProcessName() {
        assertEquals("remote", ProcessNames.safeFileSuffix("com.example.app:remote", "com.example.app"))
    }

    @Test
    fun safeFileSuffixRemovesUnsafeCharacters() {
        assertEquals("private_process", ProcessNames.safeFileSuffix("private/process", "com.example.app"))
    }
}
