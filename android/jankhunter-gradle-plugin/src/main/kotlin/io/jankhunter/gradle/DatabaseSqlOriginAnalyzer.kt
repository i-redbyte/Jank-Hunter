package io.jankhunter.gradle

import io.jankhunter.sql.SqlNormalizer
import org.objectweb.asm.Type
import org.objectweb.asm.tree.LdcInsnNode
import org.objectweb.asm.tree.MethodInsnNode
import org.objectweb.asm.tree.MethodNode
import org.objectweb.asm.tree.analysis.Analyzer
import org.objectweb.asm.tree.analysis.SourceInterpreter

internal data class DatabaseInvocationOrigin(
    val owner: String,
    val name: String,
    val descriptor: String,
    val normalizedLiteral: String?,
)

/**
 * Conservatively proves only literals that feed the SQL argument of this exact invocation.
 * Any merged/control-flow/local source stays unknown and is normalized from the saved argument at runtime.
 */
internal fun analyzeDatabaseInvocationOrigins(
    className: String,
    method: MethodNode,
): List<DatabaseInvocationOrigin> {
    val instructions = method.instructions.toArray()
    val methodCalls = instructions.count { it is MethodInsnNode }
    if (methodCalls == 0) return emptyList()
    val requiresSourceFrames = instructions.any { instruction ->
        instruction is MethodInsnNode &&
            databaseQueryArgumentIndex(instruction.owner, instruction.name, instruction.desc) != null
    }
    val frames = if (requiresSourceFrames) {
        runCatching {
            Analyzer(SourceInterpreter()).analyze(className, method)
        }.getOrNull()
    } else {
        null
    }
    val result = ArrayList<DatabaseInvocationOrigin>(methodCalls)
    for (index in instructions.indices) {
        val invocation = instructions[index] as? MethodInsnNode ?: continue
        val queryArgument = databaseQueryArgumentIndex(invocation.owner, invocation.name, invocation.desc)
        val literal = if (queryArgument == null || frames == null) {
            null
        } else {
            normalizedLiteralArgument(invocation, queryArgument, frames[index])
        }
        result.add(DatabaseInvocationOrigin(
            invocation.owner,
            invocation.name,
            invocation.desc,
            literal,
        ))
    }
    return result
}

private fun normalizedLiteralArgument(
    invocation: MethodInsnNode,
    queryArgument: Int,
    frame: org.objectweb.asm.tree.analysis.Frame<org.objectweb.asm.tree.analysis.SourceValue>?,
): String? {
    if (frame == null) return null
    val argumentCount = Type.getArgumentTypes(invocation.desc).size
    val stackIndex = frame.stackSize - argumentCount + queryArgument
    if (stackIndex < 0 || stackIndex >= frame.stackSize) return null
    val source = frame.getStack(stackIndex)
    if (source.insns.size != 1) return null
    val value = (source.insns.first() as? LdcInsnNode)?.cst as? String ?: return null
    return SqlNormalizer.normalize(value)
}
