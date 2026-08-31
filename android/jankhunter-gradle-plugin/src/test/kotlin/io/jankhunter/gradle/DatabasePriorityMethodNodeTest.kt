package io.jankhunter.gradle

import org.junit.Assert.assertSame
import org.junit.Test
import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes

class DatabasePriorityMethodNodeTest {
    @Test
    fun priorityHandlersArePromotedAheadOfExistingCatchBlocks() {
        val method = DatabasePriorityMethodNode(
            access = Opcodes.ACC_PUBLIC,
            name = "query",
            descriptor = "()V",
            signature = null,
            exceptions = null,
        )
        val start = Label()
        val end = Label()
        val userHandler = Label()
        val databaseHandler = Label()

        method.visitTryCatchBlock(start, end, userHandler, "java/lang/RuntimeException")
        val userBlock = method.tryCatchBlocks.single()
        method.recordPriorityHandler(databaseHandler)
        method.visitTryCatchBlock(start, end, databaseHandler, "java/lang/Throwable")
        val databaseBlock = method.tryCatchBlocks.last()
        method.promotePriorityBlocks()

        assertSame(databaseBlock, method.tryCatchBlocks[0])
        assertSame(userBlock, method.tryCatchBlocks[1])
    }
}
