package io.jankhunter.gradle

import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
import org.objectweb.asm.commons.AdviceAdapter

/** Reads public WorkRequest metadata in the guarded runtime boundary after the original enqueue returns. */
internal class WorkSubmissionBytecode(
    private val visitor: AdviceAdapter,
    private val source: String,
    private val causalSite: CausalSourceSite? = null,
) {
    fun emit(result: Int, arguments: IntArray) = with(visitor) {
        val done = Label()
        loadLocal(result)
        ifNull(done)
        val requestIndex = if (arguments.size == 3) 2 else 0
        loadLocal(arguments[requestIndex])
        if (arguments.size == 3) {
            loadLocal(arguments[0])
            loadLocal(arguments[1])
        } else {
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ACONST_NULL)
        }
        visitLdcInsn(source)
        visitMethodInsn(Opcodes.INVOKESTATIC, "io/jankhunter/runtime/JankHunterHooks",
            "observeWorkSubmissionRequests",
            "(Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/String;)V", false)
        causalSite?.let { site ->
            loadLocal(arguments[requestIndex])
            visitLdcInsn(site.id)
            visitLdcInsn(site.method)
            push(site.line)
            push(site.origin.wireValue)
            visitMethodInsn(Opcodes.INVOKESTATIC, "io/jankhunter/runtime/JankHunterHooks",
                "observeCausalWorkSubmissionRequests",
                "(Ljava/lang/Object;JLjava/lang/String;II)V", false)
        }
        visitLabel(done)
    }
}
