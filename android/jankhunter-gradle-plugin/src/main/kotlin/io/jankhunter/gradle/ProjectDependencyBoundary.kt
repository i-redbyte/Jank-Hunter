package io.jankhunter.gradle

import org.gradle.api.artifacts.ProjectDependency

/** ProjectDependency.path was added after Gradle 8.0; older versions expose dependencyProject. */
internal fun ProjectDependency.compatibleProjectPath(): String {
    val modern = ProjectDependency::class.java.methods.firstOrNull { it.name == "getPath" && it.parameterCount == 0 }
    if (modern != null) return modern.invoke(this) as String
    val legacy = ProjectDependency::class.java.getMethod("getDependencyProject").invoke(this)
    return org.gradle.api.Project::class.java.getMethod("getPath").invoke(legacy) as String
}
