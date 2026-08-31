package io.jankhunter.gradle

import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationFactoryArchitectureTest {
    @Test
    fun classSelectionIsOwnedByDedicatedPolicy() {
        val selector = Class.forName("io.jankhunter.gradle.InstrumentationClassSelector")
        assertTrue(selector.declaredMethods.any { method -> method.name == "evaluate" })

        val selectionMethods = setOf(
            "autoInitMatches",
            "runtimeInstrumentationMatches",
            "dependencyInjectionAnalysisMatches",
            "networkInstrumentationMatches",
            "databaseInstrumentationMatches",
        )
        assertTrue(JankHunterClassVisitorFactory::class.java.declaredMethods.none { method ->
            method.name in selectionMethods
        })
    }

    @Test
    fun instrumentationParametersAreMappedByDedicatedFactory() {
        val mapper = Class.forName("io.jankhunter.gradle.InstrumentationHookConfigFactory")
        assertTrue(mapper.declaredMethods.any { method -> method.name == "create" })
    }

    @Test
    fun methodVisitorDoesNotRetainDeadOrConstructionOnlyState() {
        val visitor = Class.forName("io.jankhunter.gradle.JankHunterMethodVisitor")
        val obsoleteFields = setOf(
            "hookApplied",
            "emittingHook",
            "accessFlags",
            "classAccessFlags",
            "kotlinMethodOrigin",
            "classHierarchy",
            "roomDaoMethod",
        )
        val actual = visitor.declaredFields.mapTo(HashSet(), java.lang.reflect.Field::getName)
        assertTrue("Obsolete method visitor state remains: ${actual intersect obsoleteFields}",
            actual.none(obsoleteFields::contains))
    }

    @Test
    fun methodVisitorLocalSlotsAreOwnedByMethodState() {
        val visitor = Class.forName("io.jankhunter.gradle.JankHunterMethodVisitor")
        val stateType = Class.forName("io.jankhunter.gradle.MethodInstrumentationState")
        assertTrue(visitor.declaredFields.any { field -> field.type == stateType })
        val displacedFields = setOf(
            "runtimeCallStartLocal",
            "annotationScopeLocal",
            "annotationOperationLocal",
            "semanticStartLocal",
            "semanticOutcomeLocal",
            "semanticKind",
            "databaseMethodStartLocal",
            "databaseMethodQueryLocal",
            "databaseMethodFingerprintLocal",
            "databaseMethodOperationLocal",
            "workerInstanceLocal",
            "workerRunAttemptLocal",
            "methodTryStart",
            "methodTryEnd",
            "methodExceptionHandler",
            "currentLine",
            "databaseInvocationOriginIndex",
            "constructorBodyEntered",
        )
        val actual = visitor.declaredFields.mapTo(HashSet(), java.lang.reflect.Field::getName)
        assertTrue("Method slot state leaked back into visitor: ${actual intersect displacedFields}",
            actual.none(displacedFields::contains))
    }
}
