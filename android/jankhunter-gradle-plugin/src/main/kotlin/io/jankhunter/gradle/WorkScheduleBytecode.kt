package io.jankhunter.gradle

import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter

/** Observes evaluated public API arguments only after the original call succeeds. */
internal object WorkScheduleBytecode {
    private const val PERIODIC = "androidx/work/PeriodicWorkRequest\$Builder"
    private val builders = setOf(PERIODIC, "androidx/work/OneTimeWorkRequest\$Builder", "androidx/work/WorkRequest\$Builder")
    private val classes = listOf("Ljava/lang/Class;", "Lkotlin/reflect/KClass;")
    private val periods = classes.flatMap { type -> listOf(
        "(${type}JLjava/util/concurrent/TimeUnit;)V",
        "(${type}JLjava/util/concurrent/TimeUnit;JLjava/util/concurrent/TimeUnit;)V",
        "(${type}Ljava/time/Duration;)V",
        "(${type}Ljava/time/Duration;Ljava/time/Duration;)V",
    ) }.toSet()

    fun matches(opcode: Int, owner: String, name: String, descriptor: String): Boolean {
        if (name == "<init>") return opcode == Opcodes.INVOKESPECIAL && owner == PERIODIC && descriptor in periods
        if (name != "setInitialDelay" || opcode != Opcodes.INVOKEVIRTUAL || owner !in builders) return false
        return descriptor in listOf(
            "(JLjava/util/concurrent/TimeUnit;)L$owner;",
            "(Ljava/time/Duration;)L$owner;",
            "(JLjava/util/concurrent/TimeUnit;)Landroidx/work/WorkRequest\$Builder;",
            "(Ljava/time/Duration;)Landroidx/work/WorkRequest\$Builder;",
        )
    }

    fun emit(visitor: AdviceAdapter, call: MethodInvocation, emitOriginal: (MethodInvocation) -> Unit) = with(visitor) {
        val types = Type.getArgumentTypes(call.descriptor)
        val args = IntArray(types.size)
        for (i in types.indices.reversed()) {
            args[i] = newLocal(types[i])
            storeLocal(args[i], types[i])
        }
        val receiver = newLocal(Type.getObjectType(call.owner))
        storeLocal(receiver)
        loadLocal(receiver)
        types.indices.forEach { loadLocal(args[it], types[it]) }
        // An uninitialized receiver is never passed to telemetry. INVOKESPECIAL initializes aliases.
        emitOriginal(call)
        val resultType = Type.getReturnType(call.descriptor)
        val result = if (resultType != Type.VOID_TYPE) newLocal(resultType).also { storeLocal(it) } else -1
        loadLocal(receiver)
        val periodic = call.name == "<init>"
        val first = if (periodic) 1 else 0
        val duration = types[first].descriptor == "Ljava/time/Duration;"
        if (duration) {
            loadLocal(args[first])
            if (periodic) {
                if (types.size > first + 1) loadLocal(args[first + 1]) else visitInsn(Opcodes.ACONST_NULL)
            }
        } else {
            loadLocal(args[first], Type.LONG_TYPE)
            loadLocal(args[first + 1])
            if (periodic) {
                if (types.size > first + 2) {
                    loadLocal(args[first + 2], Type.LONG_TYPE)
                    loadLocal(args[first + 3])
                } else {
                    push(-1L)
                    visitInsn(Opcodes.ACONST_NULL)
                }
            }
        }
        val hook = if (periodic) "observeWorkBuilderPeriod" else "observeWorkBuilderDelay"
        val descriptor = if (duration) {
            if (periodic) "(Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;)V" else "(Ljava/lang/Object;Ljava/lang/Object;)V"
        } else {
            if (periodic) "(Ljava/lang/Object;JLjava/lang/Object;JLjava/lang/Object;)V" else "(Ljava/lang/Object;JLjava/lang/Object;)V"
        }
        emitOriginal(MethodInvocation(Opcodes.INVOKESTATIC, "io/jankhunter/runtime/JankHunterHooks", hook + if (duration) "DurationCall" else "Call", descriptor, false))
        if (result >= 0) loadLocal(result)
    }
}
