package io.jankhunter.gradle

import org.objectweb.asm.Opcodes

internal enum class SemanticHookKind(val id: Int) {
    COMPOSE(1),
    ROOM_DAO(5),
    WORKER(6),
}

internal class SemanticInstrumentationPolicy(
    constructor: Boolean,
    access: Int,
    private val composeTracing: Boolean,
    private val workerTracing: Boolean,
    private val roomDaoMethod: Boolean,
    private val methodName: String,
    private val methodDescriptor: String,
    private val hierarchy: Set<String>,
) {
    private val generatedMethod = constructor || access and (Opcodes.ACC_SYNTHETIC or Opcodes.ACC_BRIDGE) != 0

    fun select(composable: Boolean): SemanticHookKind? {
        if (generatedMethod) return null
        if (composeTracing && composable) return SemanticHookKind.COMPOSE
        if (roomDaoMethod) return SemanticHookKind.ROOM_DAO
        if (workerTracing && isSynchronousWorkerMethod()) return SemanticHookKind.WORKER
        return null
    }

    private fun isSynchronousWorkerMethod(): Boolean {
        return methodName == "doWork" &&
            methodDescriptor == WORKER_DO_WORK_DESCRIPTOR &&
            ANDROIDX_WORKER in hierarchy
    }

    private companion object {
        const val ANDROIDX_WORKER = "androidx/work/Worker"
        const val WORKER_DO_WORK_DESCRIPTOR = "()Landroidx/work/ListenableWorker${'$'}Result;"
    }
}
