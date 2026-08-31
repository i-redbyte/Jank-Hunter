package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test
import org.objectweb.asm.Opcodes

class SemanticInstrumentationPolicyTest {
    @Test
    fun selectsComposeRoomAndSynchronousWorkerKinds() {
        assertEquals(
            SemanticHookKind.COMPOSE,
            policy(composeTracing = true).select(composable = true),
        )
        assertEquals(
            SemanticHookKind.ROOM_DAO,
            policy(roomDaoMethod = true).select(composable = false),
        )
        assertEquals(
            SemanticHookKind.WORKER,
            policy(
                workerTracing = true,
                methodName = "doWork",
                descriptor = "()Landroidx/work/ListenableWorker\$Result;",
                hierarchy = setOf("androidx/work/Worker"),
            ).select(composable = false),
        )
    }

    @Test
    fun rejectsConstructorAndGeneratedMethods() {
        assertEquals(null, policy(constructor = true, composeTracing = true).select(composable = true))
        assertEquals(
            null,
            policy(access = Opcodes.ACC_SYNTHETIC, roomDaoMethod = true).select(composable = false),
        )
    }

    private fun policy(
        constructor: Boolean = false,
        access: Int = Opcodes.ACC_PUBLIC,
        composeTracing: Boolean = false,
        workerTracing: Boolean = false,
        roomDaoMethod: Boolean = false,
        methodName: String = "method",
        descriptor: String = "()V",
        hierarchy: Set<String> = emptySet(),
    ): SemanticInstrumentationPolicy {
        return SemanticInstrumentationPolicy(
            constructor,
            access,
            composeTracing,
            workerTracing,
            roomDaoMethod,
            methodName,
            descriptor,
            hierarchy,
        )
    }
}
