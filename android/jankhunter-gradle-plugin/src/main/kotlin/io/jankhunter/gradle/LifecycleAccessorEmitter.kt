package io.jankhunter.gradle

import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodNode

/** Direct field references make capture visible to R8 without keeping names or reflecting fields. */
internal class LifecycleAccessorEmitter(
    private val node: ClassNode,
    private val kind: LifecycleTargetKind,
    private val parentAccessor: Boolean,
    private val bindingAvailable: Boolean,
    private val lazyAvailable: Boolean,
    private val captureFragmentFields: Boolean = kind == LifecycleTargetKind.FRAGMENT,
    private val fragmentViewOwner: String? = null,
) {
    fun emit() {
        check(node.methods.none { it.name == KIND_METHOD || it.name == VISIT_METHOD }) {
            "Lifecycle accessor name collision in ${node.name}"
        }
        if (ACCESSOR !in node.interfaces) node.interfaces.add(ACCESSOR)
        node.methods.add(MethodNode(Opcodes.ACC_PUBLIC or Opcodes.ACC_SYNTHETIC, KIND_METHOD, "()I", null, null).apply {
            visitCode()
            visitLdcInsn(kind.code)
            visitInsn(Opcodes.IRETURN)
            visitMaxs(1, 1)
            visitEnd()
        })
        node.methods.add(MethodNode(Opcodes.ACC_PUBLIC or Opcodes.ACC_SYNTHETIC, VISIT_METHOD, VISIT_DESCRIPTOR, null, null).apply {
            visitCode()
            if (parentAccessor) {
                visitVarInsn(Opcodes.ALOAD, 0)
                visitVarInsn(Opcodes.ALOAD, 1)
                visitVarInsn(Opcodes.ALOAD, 2)
                visitMethodInsn(Opcodes.INVOKESPECIAL, node.superName, VISIT_METHOD, VISIT_DESCRIPTOR, false)
            }
            fragmentViewOwner?.let { owner ->
                visitVarInsn(Opcodes.ALOAD, 1)
                visitVarInsn(Opcodes.ALOAD, 0)
                visitMethodInsn(Opcodes.INVOKEVIRTUAL, owner, "getView", "()Landroid/view/View;", false)
                visitVarInsn(Opcodes.ALOAD, 2)
                visitMethodInsn(Opcodes.INVOKEINTERFACE, SINK, "accept", ACCEPT_DESCRIPTOR, true)
            }
            node.fields.filter {
                captureFragmentFields && it.access and Opcodes.ACC_STATIC == 0 && it.desc.startsWith('L')
            }.forEach { field ->
                visitVarInsn(Opcodes.ALOAD, 0)
                visitFieldInsn(Opcodes.GETFIELD, node.name, field.name, field.desc)
                // Uniform local type keeps every merge frame independent of consumer class loading.
                visitTypeInsn(Opcodes.CHECKCAST, "java/lang/Object")
                visitVarInsn(Opcodes.ASTORE, 3)
                captureValue(this)
            }
            visitInsn(Opcodes.RETURN)
            visitMaxs(3, 4)
            visitEnd()
        })
    }

    private fun captureValue(method: MethodVisitor) = with(method) {
        val end = Label()
        if (lazyAvailable) {
            val initialized = Label()
            visitVarInsn(Opcodes.ALOAD, 3)
            visitTypeInsn(Opcodes.INSTANCEOF, "kotlin/Lazy")
            visitJumpInsn(Opcodes.IFEQ, initialized)
            visitVarInsn(Opcodes.ALOAD, 3)
            visitTypeInsn(Opcodes.CHECKCAST, "kotlin/Lazy")
            visitMethodInsn(Opcodes.INVOKEINTERFACE, "kotlin/Lazy", "isInitialized", "()Z", true)
            visitJumpInsn(Opcodes.IFEQ, end)
            visitVarInsn(Opcodes.ALOAD, 3)
            visitTypeInsn(Opcodes.CHECKCAST, "kotlin/Lazy")
            visitMethodInsn(Opcodes.INVOKEINTERFACE, "kotlin/Lazy", "getValue", "()Ljava/lang/Object;", true)
            visitVarInsn(Opcodes.ASTORE, 3)
            mergeFrame(this, initialized)
        }
        if (bindingAvailable) {
            val notBinding = Label()
            visitVarInsn(Opcodes.ALOAD, 3)
            visitTypeInsn(Opcodes.INSTANCEOF, BINDING)
            visitJumpInsn(Opcodes.IFEQ, notBinding)
            acceptValue(this)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitVarInsn(Opcodes.ALOAD, 3)
            visitTypeInsn(Opcodes.CHECKCAST, BINDING)
            visitMethodInsn(Opcodes.INVOKEINTERFACE, BINDING, "getRoot", "()Landroid/view/View;", true)
            visitVarInsn(Opcodes.ALOAD, 2)
            visitMethodInsn(Opcodes.INVOKEINTERFACE, SINK, "accept", ACCEPT_DESCRIPTOR, true)
            visitJumpInsn(Opcodes.GOTO, end)
            mergeFrame(this, notBinding)
        }
        visitVarInsn(Opcodes.ALOAD, 3)
        visitTypeInsn(Opcodes.INSTANCEOF, "android/view/View")
        visitJumpInsn(Opcodes.IFEQ, end)
        acceptValue(this)
        mergeFrame(this, end)
    }

    private fun acceptValue(method: MethodVisitor) = with(method) {
        visitVarInsn(Opcodes.ALOAD, 1)
        visitVarInsn(Opcodes.ALOAD, 3)
        visitVarInsn(Opcodes.ALOAD, 2)
        visitMethodInsn(Opcodes.INVOKEINTERFACE, SINK, "accept", ACCEPT_DESCRIPTOR, true)
    }

    private fun mergeFrame(method: MethodVisitor, label: Label) = with(method) {
        visitLabel(label)
        visitFrame(Opcodes.F_FULL, 4, arrayOf(node.name, SINK, "java/lang/String", "java/lang/Object"), 0, emptyArray())
    }

    companion object {
        const val ACCESSOR = "io/jankhunter/runtime/JankHunterLifecycleAccessorV1"
        const val SINK = "io/jankhunter/runtime/JankHunterLifecycleTargetSinkV1"
        const val BINDING = "androidx/viewbinding/ViewBinding"
        const val KIND_METHOD = "jankHunterLifecycleKindV1"
        const val VISIT_METHOD = "jankHunterVisitLifecycleTargetsV1"
        const val VISIT_DESCRIPTOR = "(L$SINK;Ljava/lang/String;)V"
        private const val ACCEPT_DESCRIPTOR = "(Ljava/lang/Object;Ljava/lang/String;)V"
    }
}
