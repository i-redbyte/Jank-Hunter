package io.jankhunter.gradle

import org.objectweb.asm.Opcodes

/** Fail at build time rather than emitting calls that fail with linkage errors on a device. */
internal object LifecycleRuntimeAbi {
    fun validate(index: LifecycleClassIndex) {
        interfaceMethod(index, LifecycleAccessorEmitter.ACCESSOR, LifecycleAccessorEmitter.KIND_METHOD + "()I")
        interfaceMethod(index, LifecycleAccessorEmitter.ACCESSOR, LifecycleAccessorEmitter.VISIT_METHOD + LifecycleAccessorEmitter.VISIT_DESCRIPTOR)
        interfaceMethod(index, LifecycleAccessorEmitter.SINK, "accept(Ljava/lang/Object;Ljava/lang/String;)V")
        for (descriptor in listOf(
            "(Ljava/lang/Object;Ljava/lang/String;Ljava/lang/String;)V",
            "(Ljava/lang/Object;ILjava/lang/String;Ljava/lang/String;)V",
        )) {
            val owner = "io/jankhunter/runtime/JankHunterHooks"
            val header = requireHeader(index, owner)
            requireAbi(header.access and Opcodes.ACC_INTERFACE == 0, owner)
            val signature = "watchLifecycleObject$descriptor"
            val flags = header.accessorMethods[signature] ?: 0
            requireAbi(flags and (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC) == (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC) &&
                flags and Opcodes.ACC_ABSTRACT == 0, "$owner.$signature")
        }
    }

    private fun interfaceMethod(index: LifecycleClassIndex, owner: String, signature: String) {
        val header = requireHeader(index, owner)
        requireAbi(header.access and Opcodes.ACC_INTERFACE != 0, owner)
        val flags = header.accessorMethods[signature] ?: 0
        requireAbi(flags and Opcodes.ACC_PUBLIC != 0 && flags and Opcodes.ACC_STATIC == 0, "$owner.$signature")
    }

    private fun requireHeader(index: LifecycleClassIndex, owner: String): LifecycleClassHeader {
        val header = index.header(owner)
        requireAbi(header != null && header.access and Opcodes.ACC_PUBLIC != 0, owner)
        return checkNotNull(header)
    }

    private fun requireAbi(valid: Boolean, symbol: String) {
        check(valid) { "Incompatible Jank Hunter lifecycle runtime ABI V1: $symbol; update the runtime artifact" }
    }
}
