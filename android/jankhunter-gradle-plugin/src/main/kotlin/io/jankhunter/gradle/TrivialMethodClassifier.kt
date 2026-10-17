package io.jankhunter.gradle

import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.tree.AbstractInsnNode
import org.objectweb.asm.tree.FieldInsnNode
import org.objectweb.asm.tree.MethodNode
import org.objectweb.asm.tree.VarInsnNode

/** Conservative body proof: no calls, branches, monitors, volatile or external field accesses. */
internal object TrivialMethodClassifier {
    fun classify(method: MethodNode, owner: String, plainFields: Map<String, String>): MethodFilterDecision? {
        if (method.name.startsWith("<") || method.access and
            (Opcodes.ACC_STATIC or Opcodes.ACC_SYNCHRONIZED or Opcodes.ACC_NATIVE or Opcodes.ACC_ABSTRACT) != 0 ||
            method.tryCatchBlocks.isNotEmpty()) return null
        val instructions = ArrayList<AbstractInsnNode>(4)
        for (instruction in method.instructions) {
            if (instruction.opcode < 0) continue
            if (instructions.size == 4) return null
            instructions.add(instruction)
        }
        val result = Type.getReturnType(method.desc)
        val arguments = Type.getArgumentTypes(method.desc)
        val internalOwner = owner.replace('.', '/')
        if (instructions.size == 1 && instructions[0].opcode == Opcodes.RETURN && result == Type.VOID_TYPE) {
            return decision("trivial_empty_body")
        }
        if (instructions.size == 2 && instructions[1].opcode in Opcodes.IRETURN..Opcodes.ARETURN &&
            instructions[0].opcode in Opcodes.ACONST_NULL..Opcodes.DCONST_1) {
            return decision("trivial_constant_return")
        }
        val first = instructions.firstOrNull() as? VarInsnNode ?: return null
        if (first.opcode != Opcodes.ALOAD || first.`var` != 0) return null
        if (instructions.size == 3 && arguments.isEmpty()) {
            val field = instructions[1] as? FieldInsnNode ?: return null
            if (plain(field, internalOwner, plainFields) && field.opcode == Opcodes.GETFIELD &&
                field.desc == result.descriptor && instructions[2].opcode == result.getOpcode(Opcodes.IRETURN)) {
                return decision("trivial_field_access")
            }
        }
        if (instructions.size == 4 && arguments.size == 1 && result == Type.VOID_TYPE) {
            val value = instructions[1] as? VarInsnNode ?: return null
            val field = instructions[2] as? FieldInsnNode ?: return null
            if (value.`var` == 1 && value.opcode == arguments[0].getOpcode(Opcodes.ILOAD) &&
                plain(field, internalOwner, plainFields) && field.opcode == Opcodes.PUTFIELD &&
                field.desc == arguments[0].descriptor && instructions[3].opcode == Opcodes.RETURN) {
                return decision("trivial_field_access")
            }
        }
        return null
    }

    private fun plain(field: FieldInsnNode, owner: String, fields: Map<String, String>): Boolean =
        field.owner == owner && fields[field.name] == field.desc

    private fun decision(reason: String): MethodFilterDecision = MethodFilterDecision(listOf(reason), reason)
}
