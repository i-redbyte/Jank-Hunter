package io.jankhunter.gradle

import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.MethodNode
import org.objectweb.asm.tree.TryCatchBlockNode

/** Keeps injected database failure handlers ahead of application catch blocks. */
internal class DatabasePriorityMethodNode(
    access: Int,
    name: String,
    descriptor: String,
    signature: String?,
    exceptions: Array<out String>?,
) : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
    private val priorityHandlerLabels = ArrayList<Label>(EXPECTED_PRIORITY_HANDLERS)
    private val priorityBlocks = ArrayList<TryCatchBlockNode>(EXPECTED_PRIORITY_HANDLERS)

    fun recordPriorityHandler(handler: Label) {
        priorityHandlerLabels.add(handler)
    }

    override fun visitTryCatchBlock(
        start: Label,
        end: Label,
        handler: Label,
        type: String?,
    ) {
        super.visitTryCatchBlock(start, end, handler, type)
        if (priorityHandlerLabels.any { candidate -> candidate === handler }) {
            priorityBlocks.add(tryCatchBlocks.last())
        }
    }

    fun promotePriorityBlocks() {
        priorityBlocks.forEachIndexed { targetIndex, block ->
            val currentIndex = tryCatchBlocks.indexOf(block)
            if (currentIndex > targetIndex) {
                tryCatchBlocks.removeAt(currentIndex)
                tryCatchBlocks.add(targetIndex, block)
            }
        }
    }

    private companion object {
        const val EXPECTED_PRIORITY_HANDLERS = 2
    }
}
