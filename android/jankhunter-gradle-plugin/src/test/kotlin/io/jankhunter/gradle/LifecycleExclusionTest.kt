package io.jankhunter.gradle

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.assertThrows
import org.junit.Test
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class LifecycleExclusionTest {
    @Test fun permittedLibraryAndHiltAncestorsReceiveOnlyRequiredAccessors() {
        for (name in listOf("library/Base", "app/Hilt_Base")) {
            val base = type(name, FRAGMENT, fields = true)
            val child = type("app/Child", name)
            val transformer = LifecycleScopedTransformer(index(FRAGMENT, base, child), setOf("app/Child"), "",
                LifecycleAncestorAccessPolicy(emptySet())::allows) {}
            val node = org.objectweb.asm.tree.ClassNode()
            org.objectweb.asm.ClassReader(transformer.transform(base)).accept(node, 0)
            assertTrue(node.methods.any { it.name == LifecycleAccessorEmitter.VISIT_METHOD })
            assertTrue(node.methods.none { it.name == "onDestroy" || it.name == "onDestroyView" })
            assertTrue(!child.contentEquals(transformer.transform(child)))
        }
    }

    @Test fun ancestorPermissionPreservesExplicitPackageAndAnnotationExclusions() {
        for (ignored in listOf(false, true)) {
            val base = type("library/Base", FRAGMENT, fields = true, ignored = ignored)
            val policy = LifecycleAncestorAccessPolicy(if (ignored) emptySet() else setOf("library"))
            assertThrows(IllegalStateException::class.java) {
                LifecycleScopedTransformer(index(FRAGMENT, base, type("app/Child", "library/Base")),
                    setOf("app/Child"), "", policy::allows) {}
            }
        }
    }

    @Test fun frameworkDialogFragmentFieldsDoNotRequireAnApplicationBindingAccessor() {
        val dialog = type("androidx/fragment/app/DialogFragment", FRAGMENT, fields = true)
        val compat = type("androidx/appcompat/app/AppCompatDialogFragment", "androidx/fragment/app/DialogFragment", fields = true)
        val child = type("app/Dialog", "androidx/appcompat/app/AppCompatDialogFragment", fields = true)
        val transformer = LifecycleScopedTransformer(index(FRAGMENT, dialog, compat, child), setOf("app/Dialog"), "") {}
        assertArrayEquals(dialog, transformer.transform(dialog))
        assertArrayEquals(compat, transformer.transform(compat))
        assertTrue(!child.contentEquals(transformer.transform(child)))
    }

    @Test fun excludedFragmentFieldsFailBeforeTransformation() {
        for (ignored in listOf(false, true)) {
            val base = type("app/Base", FRAGMENT, fields = true, ignored = ignored)
            val index = index(FRAGMENT, base, type("app/Child", "app/Base"))
            val error = assertThrows(IllegalStateException::class.java) {
                LifecycleScopedTransformer(index, setOf("app/Child"), "") {}
            }
            assertTrue(error.message.orEmpty().contains("app/Base"))
            assertTrue(error.message.orEmpty().contains("JankHunterBindingAccessor"))
        }
    }

    @Test fun harmlessExcludedAncestorIsByteIdentical() {
        val base = type("app/Base", MODEL, ignored = true)
        val child = type("app/Child", "app/Base")
        val transformer = LifecycleScopedTransformer(index(MODEL, base, child), setOf("app/Child"), "") {}
        assertArrayEquals(base, transformer.transform(base))
        assertTrue(!child.contentEquals(transformer.transform(child)))
    }

    @Test fun explicitBindingAccessorAllowsExcludedFieldsWithoutRewritingAncestor() {
        val base = type("app/Base", FRAGMENT, fields = true, ignored = true)
        val child = type("app/Child", "app/Base", explicit = true)
        val transformer = LifecycleScopedTransformer(index(FRAGMENT, base, child), setOf("app/Child"), "") {}
        assertArrayEquals(base, transformer.transform(base))
        assertTrue(!child.contentEquals(transformer.transform(child)))
    }

    @Test fun finalCallbackInExcludedAncestorFailsEvenWithExplicitBindingAccessor() {
        val base = type("app/Base", MODEL, finalCallback = true, ignored = true)
        val error = assertThrows(IllegalStateException::class.java) {
            LifecycleScopedTransformer(index(MODEL, base, type("app/Child", "app/Base", explicit = true)), setOf("app/Child"), "") {}
        }
        assertTrue(error.message.orEmpty().contains("app/Base.onCleared"))
        assertTrue(error.message.orEmpty().contains("excluded"))
    }

    @Test fun accessorChainCrossesUnmodifiedExcludedAncestor() {
        val grandparent = type("app/Grandparent", FRAGMENT, fields = true)
        val gap = type("app/Gap", "app/Grandparent", ignored = true)
        val child = type("app/Child", "app/Gap")
        val transformer = LifecycleScopedTransformer(index(FRAGMENT, grandparent, gap, child),
            setOf("app/Grandparent", "app/Child"), "") {}
        assertArrayEquals(gap, transformer.transform(gap))
        val node = org.objectweb.asm.tree.ClassNode()
        org.objectweb.asm.ClassReader(transformer.transform(child)).accept(node, 0)
        val visitor = node.methods.single { it.name == LifecycleAccessorEmitter.VISIT_METHOD }
        assertTrue(visitor.instructions.asSequence().filterIsInstance<org.objectweb.asm.tree.MethodInsnNode>().any {
            it.opcode == Opcodes.INVOKESPECIAL && it.owner == "app/Gap" && it.name == LifecycleAccessorEmitter.VISIT_METHOD
        })
    }

    private fun index(root: String, vararg program: ByteArray) = LifecycleClassIndex().apply {
        add(type(root, "java/lang/Object", callbacks = true), false)
        program.forEach { add(it, true) }
    }

    private fun type(name: String, parent: String, fields: Boolean = false, ignored: Boolean = false,
        explicit: Boolean = false, finalCallback: Boolean = false, callbacks: Boolean = false): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, parent,
            if (explicit) arrayOf("io/jankhunter/runtime/JankHunterBindingAccessor") else null)
        if (ignored) writer.visitAnnotation("Lio/jankhunter/annotations/JankHunterIgnore;", false).visitEnd()
        if (fields) writer.visitField(Opcodes.ACC_PRIVATE, "binding", "Ljava/lang/Object;", null, null).visitEnd()
        if (callbacks || finalCallback) {
            for (method in if (name == FRAGMENT) listOf("onDestroy", "onDestroyView") else listOf("onCleared")) {
                writer.visitMethod(Opcodes.ACC_PUBLIC or (if (finalCallback) Opcodes.ACC_FINAL else 0), method, "()V", null, null).apply {
                    visitCode(); visitInsn(Opcodes.RETURN); visitMaxs(0, 1); visitEnd()
                }
            }
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private companion object {
        const val FRAGMENT = "androidx/fragment/app/Fragment"
        const val MODEL = "androidx/lifecycle/ViewModel"
    }
}
