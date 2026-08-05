package io.jankhunter.sample

import android.content.pm.PackageManager
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import io.jankhunter.sample.manual.ManualAction
import io.jankhunter.sample.manual.ManualControlsUiFactory
import io.jankhunter.sample.manual.ManualControlsUiState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SampleArchitectureTest {
    @Test
    fun composeHostRendersAfterActivityRecreation() {
        ActivityScenario.launch(MainActivity::class.java).use { scenario ->
            scenario.recreate()

            scenario.onActivity { activity ->
                assertFalse(activity.isFinishing)
            }
        }
    }

    @Test
    fun manifestContainsOnlyHostAndMemoryFixtureActivities() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val packageInfo = context.packageManager.getPackageInfo(
            context.packageName,
            PackageManager.GET_ACTIVITIES,
        )
        val activities = packageInfo.activities
            .orEmpty()
            .map { it.name }
            .filter { it.startsWith(context.packageName) }
            .toSet()

        assertEquals(
            setOf(MainActivity::class.java.name, MemoryScenarioActivity::class.java.name),
            activities,
        )
    }

    @Test
    fun manualCatalogContainsEveryActionExactlyOnce() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val dispatchedActions = mutableListOf<ManualAction>()
        val sections = ManualControlsUiFactory.create(
            context = context,
            state = ManualControlsUiState("ready", "enabled", "available"),
            onAction = dispatchedActions::add,
        )

        sections.flatMap { it.actions }.forEach { it.onClick() }

        assertEquals(ManualAction.entries.size, dispatchedActions.size)
        assertEquals(ManualAction.entries.toSet(), dispatchedActions.toSet())
    }
}
