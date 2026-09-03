package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import java.lang.reflect.Modifier

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
    fun classSelectorDoesNotAllocateMatchersInEvaluationHotPath() {
        val selector = Class.forName("io.jankhunter.gradle.InstrumentationClassSelector")
        var matcherAllocations = 0
        val resourceName = selector.name.replace('.', '/') + ".class"
        val bytecode = requireNotNull(selector.classLoader.getResourceAsStream(resourceName))

        bytecode.use { input ->
            ClassReader(input).accept(
                object : ClassVisitor(Opcodes.ASM9) {
                    override fun visitMethod(
                        access: Int,
                        name: String?,
                        descriptor: String?,
                        signature: String?,
                        exceptions: Array<out String>?,
                    ): MethodVisitor {
                        return object : MethodVisitor(Opcodes.ASM9) {
                            override fun visitTypeInsn(opcode: Int, type: String) {
                                if (opcode == Opcodes.NEW && type == INSTRUMENTATION_MATCHER) {
                                    matcherAllocations++
                                }
                            }
                        }
                    }
                },
                0,
            )
        }
        assertEquals(0, matcherAllocations)
    }

    @Test
    fun asmFactoriesKeepNoInstanceStateThatBreaksGradleIsolation() {
        listOf(
            JankHunterClassVisitorFactory::class.java,
            JankHunterLifecycleClassVisitorFactory::class.java,
        ).forEach { factory ->
            val instanceFields = factory.declaredFields.filterNot { field -> Modifier.isStatic(field.modifiers) }
            assertTrue(
                "${factory.simpleName} instance state breaks Gradle transform isolation: $instanceFields",
                instanceFields.isEmpty(),
            )
        }
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

    @Test
    fun androidComponentBytecodeIsOwnedByDedicatedEmitter() {
        val visitor = Class.forName("io.jankhunter.gradle.JankHunterMethodVisitor")
        val emitter = Class.forName("io.jankhunter.gradle.AndroidComponentMethodEmitter")
        assertTrue(visitor.declaredFields.any { field -> field.type == emitter })

        val displacedMethods = setOf(
            "emitServiceCallbackEnter",
            "emitServiceCallbackExit",
            "emitServiceForegroundCall",
            "emitReceiverCallbackEnter",
            "emitReceiverCallbackExit",
            "emitReceiverGoAsync",
            "emitReceiverAsyncFinish",
            "emitBinderServerEnter",
            "emitBinderServerExit",
            "emitBinderClientTransaction",
        )
        val actual = visitor.declaredMethods.mapTo(HashSet(), java.lang.reflect.Method::getName)
        assertTrue(
            "Android component bytecode leaked back into method visitor: ${actual intersect displacedMethods}",
            actual.none(displacedMethods::contains),
        )
    }

    @Test
    fun androidComponentEmitterDoesNotDependOnSelectionOrDiagnosticsPolicies() {
        val emitter = Class.forName("io.jankhunter.gradle.AndroidComponentMethodEmitter")
        val forbidden = setOf(
            "HookConfig",
            "InstrumentationDiagnosticsClassBuilder",
            "InstrumentationClassSelector",
            "ClassHierarchyResolver",
        )
        val dependencies = emitter.declaredFields.map { field -> field.type.simpleName }
        assertTrue(
            "Android component emitter crossed analysis boundary: ${dependencies.filter(forbidden::contains)}",
            dependencies.none(forbidden::contains),
        )
    }

    private companion object {
        const val INSTRUMENTATION_MATCHER = "io/jankhunter/gradle/InstrumentationMatcher"
    }
}
