package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterConfigurationDiagnosticsTest {
    @Test
    fun effectiveReportSeparatesValuesFromResolutionDetails() {
        val project = ProjectBuilder.builder().build()
        val extension = project.objects.newInstance(JankHunterExtension::class.java)
        extension.buildType("debug", JankHunterProfile.FULL) {
            disable(JankHunterFeature.WEBSOCKETS)
            disable(JankHunterFeature.SQLITE)
        }
        val configuration = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        val effective = JankHunterConfigurationReport.effective(configuration)
        val details = JankHunterConfigurationReport.details(configuration)

        assertTrue(effective.contains("Profile: FULL"))
        assertTrue(effective.contains("growthAnalytics = true"))
        assertTrue(effective.contains("websockets = false"))
        assertTrue(effective.contains("databaseTracing = false"))
        assertFalse(effective.contains("<-"))
        assertTrue(details.contains("effective value = false"))
        assertTrue(details.contains("source = buildType(debug)"))
        assertFalse(details.contains("<-"))
    }

    @Test
    fun jsonExportContainsStableMachineReadableKeys() {
        val project = ProjectBuilder.builder().build()
        val extension = project.objects.newInstance(JankHunterExtension::class.java)
        val configuration = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        val json = JankHunterConfigurationReport.json(configuration)

        assertTrue(json.contains("\"variant\":\"debug\""))
        assertTrue(json.contains("\"profile\":\"BALANCED\""))
        assertTrue(json.contains("\"growthAnalytics\":true"))
        assertTrue(json.contains("\"features\""))
        assertTrue(json.contains("\"warnings\""))
    }

    @Test
    fun releaseValidationReturnsEveryActionableProblemAtOnce() {
        val project = ProjectBuilder.builder().build()
        val extension = project.objects.newInstance(JankHunterExtension::class.java)
        extension.profile.set(JankHunterProfile.FULL)
        extension.release { }
        val configuration = extension.resolve(JankHunterVariantIdentity("release", "release"))

        val failures = JankHunterConfigurationValidation.failures(configuration)
        val message = failures.joinToString("\n")

        assertTrue(message.contains("privacyReviewed()"))
        assertTrue(message.contains("performanceBudget(file(...))"))
        assertTrue(message.contains("allowSecondaryProcesses()"))
    }

    @Test
    fun mainOnlyReleaseDoesNotRequireSecondaryProcessApproval() {
        val project = ProjectBuilder.builder().build()
        val budget = project.file("release-budget.txt").apply {
            writeText(JankHunterConfigurationValidation.PERFORMANCE_BUDGET_MARKER)
        }
        val extension = project.objects.newInstance(JankHunterExtension::class.java)
        extension.profile.set(JankHunterProfile.FULL)
        extension.processes.set(JankHunterProcesses.MAIN_ONLY)
        extension.release {
            privacyReviewed()
            performanceBudget(budget)
        }

        val configuration = extension.resolve(JankHunterVariantIdentity("release", "release"))
        val failures = JankHunterConfigurationValidation.failures(configuration)

        assertFalse(failures.any { it.contains("allowSecondaryProcesses()") })
        assertTrue(failures.isEmpty())
    }

    @Test
    fun functionalParityReportsOnlyBehavioralDifferencesWithReadablePaths() {
        val extension = projectExtension()
        val debug = extension.resolve(JankHunterVariantIdentity("debug", "debug"))
        extension.buildType("release", JankHunterProfile.BALANCED) {
            processes.set(JankHunterProcesses.ALL)
            tuning.queueCapacity.set(7_777)
            disable(JankHunterFeature.SQLITE)
        }
        extension.release { }
        val release = extension.resolve(JankHunterVariantIdentity("release", "release"))

        val differences = JankHunterFunctionalParity.differences(debug, release).joinToString("\n")

        assertTrue(differences.contains("features.sqlite: debug=true, release=false"))
        assertTrue(differences.contains("processes: debug=MAIN_ONLY, release=ALL"))
        assertTrue(differences.contains("tuning.queueCapacity: debug=32768, release=7777"))
        assertFalse(differences.contains("releaseSafety"))
    }

    @Test
    fun heapDumpUsesTheSharedPrivacyReviewInsteadOfASecondApprovalFlag() {
        val project = ProjectBuilder.builder().build()
        val extension = project.objects.newInstance(JankHunterExtension::class.java)
        extension.buildType("debug", JankHunterProfile.TARGETED) {
            enable(JankHunterFeature.HEAP_DUMPS)
            privacyReviewed()
        }

        val configuration = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        assertTrue(configuration.releaseSafety.privacyReviewed)
        assertTrue(JankHunterConfigurationValidation.failures(configuration).isEmpty())
    }

    @Test
    fun validationRejectsAmbiguousInstrumentationPackages() {
        val project = ProjectBuilder.builder().build()
        val extension = project.objects.newInstance(JankHunterExtension::class.java)
        extension.packages("com.example.shared")
        extension.excludePackages("com.example.shared")
        val configuration = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        val failures = JankHunterConfigurationValidation.failures(configuration)

        assertTrue(failures.single().contains("included and excluded simultaneously"))
    }

    @Test
    fun mainOnlyComponentAnalysisIsAllowedWithActionablePartialWarning() {
        val extension = projectExtension()
        extension.profile.set(JankHunterProfile.BALANCED)
        val configuration = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        val failures = JankHunterConfigurationValidation.failures(configuration)
        val warnings = JankHunterConfigurationValidation.warnings(configuration)

        assertTrue(failures.isEmpty())
        assertTrue(warnings.single().contains("partial"))
        assertTrue(warnings.single().contains("processes.set(ALL)"))
        assertTrue(JankHunterConfigurationReport.effective(configuration).contains("Warnings:"))
        assertTrue(JankHunterConfigurationReport.json(configuration).contains("partial"))
    }

    @Test
    fun allProcessComponentAnalysisDoesNotReportPartialCoverage() {
        val extension = projectExtension()
        extension.profile.set(JankHunterProfile.FULL)
        val configuration = extension.resolve(JankHunterVariantIdentity("debug", "debug"))

        assertTrue(JankHunterConfigurationValidation.warnings(configuration).isEmpty())
    }

    private fun projectExtension(): JankHunterExtension {
        val project = ProjectBuilder.builder().build()
        return project.objects.newInstance(JankHunterExtension::class.java)
    }
}
