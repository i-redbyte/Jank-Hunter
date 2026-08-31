package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.Opcodes

class MethodFilterClassifierTest {
    @Test
    fun diagnosticsClassifiesOnlyProvenGeneratedMethodFamilies() {
        val cases = listOf(
            Case(Opcodes.ACC_SYNTHETIC, "run", "example/Foo", "acc_synthetic"),
            Case(Opcodes.ACC_BRIDGE, "run", "example/Foo", "acc_bridge"),
            Case(0, "box-impl", "example/Foo", "value_class_impl"),
            Case(0, "run", "example/Foo\$\$ExternalSyntheticLambda0", "generated_helper_class"),
            Case(0, "run", "example/Foo", "acc_synthetic_class", classAccess = Opcodes.ACC_SYNTHETIC),
            Case(
                0,
                "values",
                "example/State",
                "java_enum_helper",
                classAccess = Opcodes.ACC_ENUM,
                descriptor = "()[Lexample/State;",
            ),
        )

        cases.forEach { case ->
            val decision = MethodFilterClassifier.classify(
                JankHunterMethodFilterMode.REPORT_ONLY,
                case.classAccess,
                case.access,
                case.className,
                case.name,
                case.descriptor,
                KotlinMethodOrigin.UNKNOWN,
            )
            assertTrue("missing ${case.category}: ${decision.categories}", case.category in decision.categories)
            assertEquals(case.category, decision.exclusionReason)
        }
    }

    @Test
    fun disabledModeDoesNotClassifyOrExclude() {
        val decision = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.NONE,
            0,
            Opcodes.ACC_SYNTHETIC,
            "example/Foo",
            "access\$200",
            "()V",
            KotlinMethodOrigin.UNKNOWN,
        )

        assertTrue(decision.categories.isEmpty())
        assertNull(decision.exclusionReason)
    }

    @Test
    fun sourceLegalNamesAreNotEnoughToExcludeUserCode() {
        val cases = listOf(
            "hashCode",
            "equals",
            "toString",
            "getBytes",
            "isReady",
            "setName",
            "access\$200",
            "copy\$default",
            "\$r8\$lambda\$42",
        )

        cases.forEach { name ->
            val decision = MethodFilterClassifier.classify(
                JankHunterMethodFilterMode.FILTER,
                0,
                0,
                "example/Foo",
                name,
                "()V",
                KotlinMethodOrigin.UNKNOWN,
            )
            assertTrue(name, decision.categories.isEmpty())
            assertNull(name, decision.exclusionReason)
        }
    }

    @Test
    fun metadataProofCanExcludeGeneratedMethodWithoutNameHeuristics() {
        val decision = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.FILTER,
            0,
            0,
            "example/Foo",
            "getValue",
            "()I",
            KotlinMethodOrigin.generated("kotlin_default_accessor"),
        )

        assertEquals(listOf("kotlin_default_accessor"), decision.categories)
        assertEquals("kotlin_default_accessor", decision.exclusionReason)
    }

    @Test
    fun kotlinDeclarationWinsOverSyntheticFlagAndGeneratedLookingName() {
        val decision = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.FILTER,
            0,
            Opcodes.ACC_SYNTHETIC,
            "example/Foo",
            "copy\$default",
            "()V",
            KotlinMethodOrigin.DECLARED,
        )

        assertTrue(decision.categories.isEmpty())
        assertNull(decision.exclusionReason)
    }

    @Test
    fun enumNamesAreExcludedOnlyWithTheJvmMandatedClassAndDescriptor() {
        val ordinary = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.FILTER,
            0,
            0,
            "example/State",
            "values",
            "()[Lexample/State;",
            KotlinMethodOrigin.UNKNOWN,
        )
        val customEnumMethod = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.FILTER,
            Opcodes.ACC_ENUM,
            0,
            "example/State",
            "values",
            "()Ljava/util/List;",
            KotlinMethodOrigin.UNKNOWN,
        )

        assertNull(ordinary.exclusionReason)
        assertNull(customEnumMethod.exclusionReason)
    }

    @Test
    fun exclusionReasonUsesStablePriorityWithoutASecondLookup() {
        val decision = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.REPORT_ONLY,
            0,
            Opcodes.ACC_BRIDGE or Opcodes.ACC_SYNTHETIC,
            "example/Foo",
            "access\$200",
            "()V",
            KotlinMethodOrigin.UNKNOWN,
        )

        assertEquals(listOf("acc_bridge", "acc_synthetic"), decision.categories)
        assertEquals("acc_bridge", decision.exclusionReason)
    }

    private data class Case(
        val access: Int,
        val name: String,
        val className: String,
        val category: String,
        val classAccess: Int = 0,
        val descriptor: String = "()V",
    )
}
