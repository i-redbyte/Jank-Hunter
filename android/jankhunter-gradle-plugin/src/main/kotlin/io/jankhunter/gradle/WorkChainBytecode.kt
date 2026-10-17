package io.jankhunter.gradle

import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
import org.objectweb.asm.commons.AdviceAdapter

/** The probe is a single guarded runtime call, safe with an outer operand stack at the original invocation. */
internal class WorkChainBytecode(
    private val visitor: AdviceAdapter,
    private val source: String,
) {
    fun emitCombine(parents: Int, result: Int) = with(visitor) {
        val done = Label()
        loadLocal(result)
        ifNull(done)
        loadLocal(parents)
        loadLocal(result)
        hook("observeWorkChainCombineRequests", "(Ljava/util/List;Ljava/lang/Object;)V")
        visitLabel(done)
    }

    fun emit(receiver: Int, result: Int, arguments: IntArray, root: Boolean) = with(visitor) {
        val done = Label()
        loadLocal(result)
        ifNull(done)
        val requestIndex = if (arguments.size == 3) 2 else 0
        if (root) {
            loadLocal(result)
            loadLocal(arguments[requestIndex])
            if (arguments.size == 3) {
                loadLocal(arguments[0])
                loadLocal(arguments[1])
            } else {
                visitInsn(Opcodes.ACONST_NULL)
                visitInsn(Opcodes.ACONST_NULL)
            }
            visitLdcInsn(source)
            hook("observeWorkChainRootRequests",
                "(Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/String;)V")
        } else {
            loadLocal(receiver)
            loadLocal(result)
            loadLocal(arguments[requestIndex])
            visitLdcInsn(source)
            hook("observeWorkChainChildRequests",
                "(Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/String;)V")
        }
        visitLabel(done)
    }

    private fun hook(name: String, descriptor: String) {
        visitor.visitMethodInsn(Opcodes.INVOKESTATIC, "io/jankhunter/runtime/JankHunterHooks", name, descriptor, false)
    }
}
