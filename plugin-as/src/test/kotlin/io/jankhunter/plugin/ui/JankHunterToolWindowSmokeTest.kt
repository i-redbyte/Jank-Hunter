package io.jankhunter.plugin.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase

class JankHunterToolWindowSmokeTest : BasePlatformTestCase() {
    fun testCreatesComponent() {
        val view = JankHunterToolWindow(project, enableBrowser = false)
        try {
            assertNotNull(view.component)
        } finally {
            view.dispose()
        }
    }
}
