package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.Opcodes

class MethodFilterClassifierTest {
    @Test
    fun diagnosticsClassifiesEveryRequestedNoisyMethodFamily() {
        val cases = listOf(
            Case(Opcodes.ACC_SYNTHETIC, "run", "()V", "example/Foo", "acc_synthetic"),
            Case(Opcodes.ACC_BRIDGE, "run", "()V", "example/Foo", "acc_bridge"),
            Case(0, "access\$200", "()V", "example/Foo", "access_helper"),
            Case(0, "copy\$default", "()V", "example/Foo", "kotlin_default"),
            Case(0, "\$r8\$lambda\$42", "()V", "example/Foo", "r8_lambda"),
            Case(0, "box-impl", "()V", "example/Foo", "value_class_impl"),
            Case(0, "hashCode", "()I", "example/Foo", "object_method"),
            Case(0, "getName", "()Ljava/lang/String;", "example/Foo", "accessor_shape"),
            Case(0, "run", "()V", "example/Foo\$\$ExternalSyntheticLambda0", "generated_helper_class"),
        )

        cases.forEach { case ->
            val decision = MethodFilterClassifier.classify(
                JankHunterMethodFilterMode.DIAGNOSTICS,
                MethodFilterClassifier.isGeneratedHelperClass(case.className),
                case.access,
                case.name,
                case.descriptor,
            )
            assertTrue("missing ${case.category}: ${decision.categories}", case.category in decision.categories)
            assertEquals(case.category, decision.exclusionReason)
        }
    }

    @Test
    fun disabledModeDoesNotClassifyOrExclude() {
        val decision = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.DISABLED,
            false,
            Opcodes.ACC_SYNTHETIC,
            "access\$200",
            "()V",
        )

        assertTrue(decision.categories.isEmpty())
        assertNull(decision.exclusionReason)
    }

    @Test
    fun accessorParserSupportsPrimitiveObjectAndArrayDescriptors() {
        val cases = listOf(
            Triple("getBytes", "()[B", true),
            Triple("isReady", "()Z", true),
            Triple("setName", "(Ljava/lang/String;)V", true),
            Triple("setBytes", "([B)V", true),
            Triple("setMatrix", "([[I)V", true),
            Triple("setPair", "(II)V", false),
            Triple("getVoid", "()V", false),
            Triple("run", "()I", false),
        )

        cases.forEach { (name, descriptor, expected) ->
            val decision = MethodFilterClassifier.classify(
                JankHunterMethodFilterMode.DIAGNOSTICS,
                false,
                0,
                name,
                descriptor,
            )
            if (expected) {
                assertTrue("$name$descriptor", "accessor_shape" in decision.categories)
            } else {
                assertFalse("$name$descriptor", "accessor_shape" in decision.categories)
            }
        }
    }

    @Test
    fun exclusionReasonUsesStablePriorityWithoutASecondLookup() {
        val decision = MethodFilterClassifier.classify(
            JankHunterMethodFilterMode.DIAGNOSTICS,
            false,
            Opcodes.ACC_BRIDGE or Opcodes.ACC_SYNTHETIC,
            "access\$200",
            "()V",
        )

        assertEquals(listOf("acc_bridge", "acc_synthetic", "access_helper"), decision.categories)
        assertEquals("acc_bridge", decision.exclusionReason)
    }

    private data class Case(
        val access: Int,
        val name: String,
        val descriptor: String,
        val className: String,
        val category: String,
    )
}
