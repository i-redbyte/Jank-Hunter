package io.jankhunter.sample

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.LaunchedEffect
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import io.jankhunter.sample.automatic.SampleRoute
import io.jankhunter.sample.automatic.ScenarioAction
import io.jankhunter.sample.automatic.ScenarioEffect
import io.jankhunter.sample.automatic.ScenarioProgressUseCase
import io.jankhunter.sample.automatic.ScenarioViewModel
import io.jankhunter.sample.manual.ManualAction
import io.jankhunter.sample.manual.ManualControlsEffect
import io.jankhunter.sample.manual.ManualControlsUiFactory
import io.jankhunter.sample.manual.ManualControlsViewModel
import io.jankhunter.sample.ui.ManualControlsScreen
import io.jankhunter.sample.ui.SampleTheme
import io.jankhunter.sample.ui.ScenarioScreen
import io.jankhunter.runtime.JankHunter
import kotlinx.coroutines.launch

internal class MainActivity : ComponentActivity() {
    private val scenarioViewModel by viewModels<ScenarioViewModel> {
        viewModelFactory {
            initializer {
                ScenarioViewModel(application as SampleApplication)
            }
        }
    }
    private val manualViewModel by viewModels<ManualControlsViewModel> {
        viewModelFactory {
            initializer {
                ManualControlsViewModel(application as SampleApplication)
            }
        }
    }
    private val memoryStageLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) {
        scenarioViewModel.onMemoryStageCompleted()
    }
    private val scenarioProgress by lazy { ScenarioProgressUseCase(resources) }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge(
            statusBarStyle = SystemBarStyle.dark(android.graphics.Color.TRANSPARENT),
            navigationBarStyle = SystemBarStyle.dark(android.graphics.Color.TRANSPARENT),
        )
        setContent {
            val scenarioState = scenarioViewModel.state.collectAsStateWithLifecycle().value
            SampleTheme {
                when (val route = scenarioState.route) {
                    is SampleRoute.Automatic -> ScenarioScreen(
                        progress = scenarioProgress(route.step),
                        step = route.step,
                        onAction = ::onScenarioAction,
                    )
                    SampleRoute.Manual -> {
                        val manualState = manualViewModel.state.collectAsStateWithLifecycle().value
                        LaunchedEffect(manualViewModel) {
                            manualViewModel.effects.collect { effect ->
                                when (effect) {
                                    ManualControlsEffect.ShareDiagnostics -> {
                                        JankHunterShareLauncher(this@MainActivity).share()
                                    }
                                }
                            }
                        }
                        BackHandler(onBack = ::finish)
                        ManualControlsScreen(
                            label = getString(R.string.manual_mode_label),
                            title = getString(R.string.manual_mode_title),
                            description = getString(R.string.manual_mode_description),
                            status = manualState.status,
                            sections = ManualControlsUiFactory.create(this, manualState, ::onManualAction),
                        )
                    }
                }
            }
        }
        collectEffects()
        scenarioViewModel.start()
    }

    override fun onPostResume() {
        super.onPostResume()
        window.decorView.post {
            JankHunter.setScreen(scenarioViewModel.currentScreenName())
        }
    }

    private fun collectEffects() {
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                launch {
                    scenarioViewModel.effects.collect { effect ->
                        when (effect) {
                            ScenarioEffect.OpenMemoryStage -> {
                                memoryStageLauncher.launch(Intent(this@MainActivity, MemoryScenarioActivity::class.java))
                            }
                            ScenarioEffect.ShareDiagnostics -> JankHunterShareLauncher(this@MainActivity).share()
                        }
                    }
                }
            }
        }
    }

    private fun onScenarioAction(action: ScenarioAction) {
        if (action == ScenarioAction.OPEN_MANUAL_MODE) {
            LeakCanaryBridge.configure()
        }
        scenarioViewModel.onAction(action)
    }

    private fun onManualAction(action: ManualAction) {
        val activityReference = if (action.requiresActivityReference) this else null
        manualViewModel.onAction(action, activityReference)
    }
}
