package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CausalSupportExclusionTest {
    @Test fun excludesJankHunterSupportModulesWithoutExcludingSampleApp() {
        listOf(
            "io/jankhunter/runtime/JankHunter",
            "io/jankhunter/annotations/JankHunterOwner",
            "io/jankhunter/gradle/JankHunterPlugin",
            "io/jankhunter/workmanager/JankHunterCoroutineWorker",
            "io/jankhunter/okhttp3/JankHunterEventListener",
        ).forEach { assertTrue(it, isJankHunterSupportClass(it)) }
        assertFalse(isJankHunterSupportClass("io/jankhunter/sample/concurrency/RetryScenarioWorker"))
        assertFalse(isJankHunterSupportClass("com/fixture/app/MainThread"))
    }
}
