package io.jankhunter.gradle

import org.objectweb.asm.Handle
import org.objectweb.asm.Type
import org.objectweb.asm.tree.InvokeDynamicInsnNode
import org.objectweb.asm.tree.MethodNode

internal enum class TaskLambdaKind { RUNNABLE, CALLABLE, CALLBACK }

/** Bootstrap proof only. Method names and the synthetic flag alone establish no task boundary. */
internal class TaskLambdaIndex(owner: String) {
    private val owner = owner.replace('.', '/')
    private val implementations = HashMap<String, TaskLambdaKind>()

    fun record(method: MethodNode) {
        for (instruction in method.instructions) {
            val call = instruction as? InvokeDynamicInsnNode ?: continue
            if (call.bsm.owner != "java/lang/invoke/LambdaMetafactory" ||
                call.bsm.name != "metafactory" && call.bsm.name != "altMetafactory") continue
            val result = Type.getReturnType(call.desc)
            if (result.sort != Type.OBJECT) continue
            val kind = when (result.internalName) {
                "java/lang/Runnable" -> TaskLambdaKind.RUNNABLE
                "java/util/concurrent/Callable" -> TaskLambdaKind.CALLABLE
                "android/view/View\$OnClickListener" -> TaskLambdaKind.CALLBACK
                else -> continue
            }
            val implementation = call.bsmArgs.getOrNull(1) as? Handle ?: continue
            if (implementation.owner != owner || implementation.name.startsWith("<")) continue
            implementations.putIfAbsent(implementation.name + implementation.desc, kind)
        }
    }

    fun kind(name: String, descriptor: String): TaskLambdaKind? = implementations[name + descriptor]
}
