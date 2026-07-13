package io.jankhunter.gradle

import java.util.Properties

internal data class JankHunterDependencyCoordinates(
    val group: String,
    val version: String,
) {
    companion object {
        private const val METADATA_RESOURCE = "io/jankhunter/gradle/jankhunter-plugin.properties"
        private const val DEFAULT_GROUP = "io.jankhunter"

        fun load(): JankHunterDependencyCoordinates {
            val properties = Properties()
            JankHunterDependencyCoordinates::class.java.classLoader
                .getResourceAsStream(METADATA_RESOURCE)
                ?.use(properties::load)

            val group = properties.getProperty("jankHunterGroup")
                ?.trim()
                ?.takeIf(String::isNotEmpty)
                ?: DEFAULT_GROUP
            val version = properties.getProperty("jankHunterVersion")
                ?.trim()
                ?.takeIf(String::isNotEmpty)
                ?: JankHunterDependencyCoordinates::class.java.`package`.implementationVersion
                ?: "unspecified"

            return JankHunterDependencyCoordinates(group, version)
        }
    }
}
