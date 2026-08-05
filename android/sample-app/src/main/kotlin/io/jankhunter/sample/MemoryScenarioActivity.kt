package io.jankhunter.sample

import android.app.Activity
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.lifecycle.lifecycleScope
import io.jankhunter.sample.automatic.AutomaticMemoryScenario
import io.jankhunter.sample.automatic.ScenarioProgressUseCase
import io.jankhunter.sample.automatic.ScenarioStep
import io.jankhunter.sample.graph.ScenarioGraphComponent
import io.jankhunter.sample.ui.SampleTheme
import io.jankhunter.sample.ui.ScenarioScreen
import io.jankhunter.runtime.JankHunter
import kotlinx.coroutines.launch

internal class MemoryScenarioActivity : ComponentActivity() {
    private val scenarioProgress by lazy { ScenarioProgressUseCase(resources) }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(android.graphics.Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(android.graphics.Color.TRANSPARENT),
        )
        setContent {
            SampleTheme {
                ScenarioScreen(
                    progress = scenarioProgress(ScenarioStep.MEMORY),
                    step = ScenarioStep.MEMORY,
                    onAction = {},
                )
            }
        }
        JankHunter.setScreen(ScenarioStep.MEMORY.screenName)
        lifecycleScope.launch {
            val sampleApplication = application as SampleApplication
            AutomaticMemoryScenario(
                ScenarioGraphComponent(sampleApplication).memory,
            ).run(this@MemoryScenarioActivity)
            setResult(Activity.RESULT_OK)
            finish()
        }
    }

    override fun onPostResume() {
        super.onPostResume()
        window.decorView.post {
            JankHunter.setScreen(ScenarioStep.MEMORY.screenName)
        }
    }
}
