package io.jankhunter.plugin.run

import com.intellij.execution.configurations.ConfigurationFactory
import com.intellij.execution.configurations.ConfigurationType
import com.intellij.openapi.project.Project
import javax.swing.Icon

class JankHunterRunConfigurationType : ConfigurationType {
    override fun getDisplayName(): String = "Jank Hunter"

    override fun getConfigurationTypeDescription(): String = "Run Jank Hunter CLI"

    override fun getIcon(): Icon? = null

    override fun getId(): String = ID

    override fun getConfigurationFactories(): Array<ConfigurationFactory> = arrayOf(JankHunterRunConfigurationFactory(this))

    companion object {
        const val ID = "JANK_HUNTER_RUN_CONFIGURATION"
    }
}

class JankHunterRunConfigurationFactory(type: ConfigurationType) : ConfigurationFactory(type) {
    override fun getId(): String = "JankHunter"

    override fun createTemplateConfiguration(project: Project): JankHunterRunConfiguration =
        JankHunterRunConfiguration(project, this, "Jank Hunter")
}
