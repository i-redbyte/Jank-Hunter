package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        return JankHunterClassVisitor(
            nextClassVisitor,
            classContext.currentClassData.className,
        )
    }

    override fun isInstrumentable(classData: ClassData): Boolean {
        val params = parameters.get()
        if (!params.methodCounters.getOrElse(false)) return false
        return InstrumentationMatcher(
            params.includePackages.getOrElse(emptyList()),
            params.excludePackages.getOrElse(emptyList()),
        ).matches(classData.className)
    }
}

private class JankHunterClassVisitor(
    next: ClassVisitor,
    private val className: String,
) : ClassVisitor(Opcodes.ASM9, next) {
    override fun visitMethod(
        access: Int,
        name: String,
        descriptor: String,
        signature: String?,
        exceptions: Array<out String>?,
    ): MethodVisitor {
        val next = super.visitMethod(access, name, descriptor, signature, exceptions)
        if (name == "<init>" || name == "<clinit>") return next
        if (access and Opcodes.ACC_ABSTRACT != 0) return next
        if (access and Opcodes.ACC_NATIVE != 0) return next
        return OwnerCounterMethodVisitor(next, ownerName(className, name))
    }

    private fun ownerName(className: String, methodName: String): String {
        return "owner.${className.replace('/', '.')}.$methodName"
    }
}

private class OwnerCounterMethodVisitor(
    next: MethodVisitor,
    private val ownerName: String,
) : MethodVisitor(Opcodes.ASM9, next) {
    override fun visitCode() {
        super.visitCode()
        visitLdcInsn(ownerName)
        visitInsn(Opcodes.LCONST_1)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            "io/jankhunter/runtime/JankHunter",
            "recordCounter",
            "(Ljava/lang/String;J)V",
            false,
        )
    }

    override fun visitMaxs(maxStack: Int, maxLocals: Int) {
        super.visitMaxs(maxStack + 3, maxLocals)
    }
}
