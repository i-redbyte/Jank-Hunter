package io.jankhunter.gradle;

import org.gradle.api.Plugin;
import org.gradle.api.Project;

public final class JankHunterPlugin implements Plugin<Project> {
    @Override
    public void apply(Project project) {
        JankHunterExtension extension = project.getExtensions().create("jankHunter", JankHunterExtension.class);

        project.getPluginManager().withPlugin("com.android.application", plugin -> configureAndroidProject(project, extension));
        project.getPluginManager().withPlugin("com.android.library", plugin -> configureAndroidProject(project, extension));
    }

    private void configureAndroidProject(Project project, JankHunterExtension extension) {
        project.getLogger().lifecycle(
                "Jank Hunter configured for build types {}. ASM instrumentation hooks will be added in the next implementation phase.",
                extension.getEnabledBuildTypes()
        );
    }
}
