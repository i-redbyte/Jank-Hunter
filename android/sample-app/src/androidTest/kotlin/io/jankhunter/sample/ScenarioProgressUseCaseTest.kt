package io.jankhunter.sample

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import io.jankhunter.sample.automatic.ScenarioProgressUseCase
import io.jankhunter.sample.automatic.ScenarioStep
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ScenarioProgressUseCaseTest {
    @Test
    fun formatsContinuousProgressForEveryScenarioScreen() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val useCase = ScenarioProgressUseCase(context.resources)

        assertEquals(ScenarioStep.entries.lastIndex, useCase.total)
        ScenarioStep.entries.forEachIndexed { index, step ->
            val expected = context.getString(
                R.string.auto_progress_format,
                index,
                ScenarioStep.entries.lastIndex,
                context.getString(step.labelRes),
            )
            assertEquals(expected, useCase(step))
        }
    }
}
