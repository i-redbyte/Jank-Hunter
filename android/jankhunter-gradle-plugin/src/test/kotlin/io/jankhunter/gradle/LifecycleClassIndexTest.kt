package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class LifecycleClassIndexTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test
    fun largeApplicationsDoNotFailAtAMetadataOrClassCountThreshold() {
        LifecycleClassIndex(temporary.newFile("large.mv")).use { index ->
            repeat(260_001) { number ->
                index.add(type("app/large/application/Feature$number", "java/lang/Object"), false)
            }
            index.add(type("app/large/application/Feature260000", "android/app/Activity"), true)
            assertEquals(LifecycleTargetKind.ACTIVITY, index.kind("app/large/application/Feature260000"))
            assertEquals(1, index.programClasses().count())
            assertEquals("app/large/application/Feature0", index.header("app/large/application/Feature0")?.name)
        }
    }

    @Test
    fun deepValidHierarchyIsNotRejectedByAnArbitraryAncestorCount() {
        val index = LifecycleClassIndex()
        repeat(4_200) { number ->
            index.add(type("app/Level$number", if (number == 0) "android/app/Activity" else "app/Level${number - 1}"), false)
        }
        assertEquals(LifecycleTargetKind.ACTIVITY, index.kind("app/Level4199"))
    }

    @Test
    fun inheritedFinalDeclarationIsResolvedWithoutLoadingTheClass() {
        val index = LifecycleClassIndex()
        index.add(type("app/Child", "library/Base"), true)
        index.add(type("library/Base", "androidx/lifecycle/ViewModel", Opcodes.ACC_PROTECTED or Opcodes.ACC_FINAL), true)
        index.add(type("androidx/lifecycle/ViewModel", "java/lang/Object", Opcodes.ACC_PROTECTED), false)
        assertEquals(LifecycleTargetKind.VIEW_MODEL, index.kind("app/Child"))
        val callback = index.callback("app/Child", "onCleared") as LifecycleCallbackResolution.Declaration
        assertEquals("library/Base", callback.owner)
        assertFalse(callback.canOverrideFrom("app/Child"))
        assertTrue(callback.hasBody)
        assertTrue(callback.programClass)
        assertEquals(
            LifecycleCallbackPlan.InstrumentDeclaration("library/Base"),
            LifecycleCallbackPlanner(index).plan("app/Child", "onCleared"),
        )
    }

    @Test
    fun privateCallbackShapedMethodIsNotALifecycleOverride() {
        val index = LifecycleClassIndex()
        index.add(type("app/Child", null, Opcodes.ACC_PRIVATE), true)
        assertTrue(LifecycleCallbackPlanner(index).plan("app/Child", "onCleared") is LifecycleCallbackPlan.Unsupported)
    }

    @Test
    fun inaccessibleFinalClasspathDeclarationIsExplicitlyUnsupported() {
        val index = LifecycleClassIndex()
        index.add(type("app/Child", "library/Base"), true)
        index.add(type("library/Base", "androidx/lifecycle/ViewModel", Opcodes.ACC_PROTECTED or Opcodes.ACC_FINAL), false)
        assertTrue(LifecycleCallbackPlanner(index).plan("app/Child", "onCleared") is LifecycleCallbackPlan.Unsupported)
    }

    @Test
    fun overrideCallsImmediateParentEvenWhenDeclarationIsFurtherUp() {
        val index = LifecycleClassIndex()
        index.add(type("app/Child", "library/Middle"), true)
        index.add(type("library/Middle", "library/Base"), false)
        index.add(type("library/Base", null, Opcodes.ACC_PROTECTED), false)
        assertEquals(
            LifecycleCallbackPlan.Override("library/Middle", Opcodes.ACC_PROTECTED),
            LifecycleCallbackPlanner(index).plan("app/Child", "onCleared"),
        )
    }

    @Test
    fun missingAncestorIsDistinctFromAnAbsentCallback() {
        val index = LifecycleClassIndex()
        index.add(type("app/Child", "missing/Parent"), true)
        assertEquals(LifecycleCallbackResolution.MissingClass("missing/Parent"), index.callback("app/Child", "onCleared"))
        index.add(type("missing/Parent", null), false)
        assertEquals(LifecycleCallbackResolution.Absent, index.callback("app/Child", "onCleared"))
    }

    @Test
    fun packagePrivateMethodCannotBeOverriddenFromAnotherPackage() {
        val index = LifecycleClassIndex()
        index.add(type("app/Child", "library/Base"), true)
        index.add(type("library/Base", null, 0), true)
        val callback = index.callback("app/Child", "onCleared") as LifecycleCallbackResolution.Declaration
        assertFalse(callback.canOverrideFrom("app/Child"))
        assertTrue(callback.canOverrideFrom("library/Child"))
    }

    @Test
    fun programDefinitionWinsOverCompileClasspathAndCyclesFailExplicitly() {
        val index = LifecycleClassIndex()
        index.add(type("app/Child", "app/Base"), false)
        index.add(type("app/Child", "android/app/Activity"), true)
        index.add(type("app/Child", "app/Base"), false)
        assertEquals(LifecycleTargetKind.ACTIVITY, index.kind("app/Child"))
        assertThrows(IllegalArgumentException::class.java) { index.add(type("app/Child", null), true) }
        index.add(type("loop/A", "loop/B"), true)
        index.add(type("loop/B", "loop/A"), true)
        assertThrows(IllegalStateException::class.java) { index.kind("loop/A") }
    }

    private fun type(name: String, parent: String?, callbackAccess: Int? = null): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, parent, null)
        callbackAccess?.let { access ->
            writer.visitMethod(access, "onCleared", "()V", null, null).apply {
                visitCode()
                visitInsn(Opcodes.RETURN)
                visitMaxs(0, 1)
                visitEnd()
            }
        }
        writer.visitEnd()
        return writer.toByteArray()
    }
}
