package io.jankhunter.gradle

import org.junit.Assert.assertThrows
import org.junit.Test
import org.objectweb.asm.Opcodes

class LifecycleRuntimeAbiSdkTest {
    @Test fun validateSdkAcceptsCompatibleLifecycleHeaders() {
        val accessor = "io/jankhunter/runtime/JankHunterLifecycleAccessorV1"
        val sink = "io/jankhunter/runtime/JankHunterLifecycleTargetSinkV1"
        val hooks = "io/jankhunter/runtime/JankHunterHooks"
        LifecycleRuntimeAbi.validateSdk(mapOf(
            accessor to HookAbiClass(
                Opcodes.ACC_PUBLIC or Opcodes.ACC_INTERFACE, "java/lang/Object", emptyList(),
                mapOf(
                    LifecycleAccessorEmitter.KIND_METHOD + "()I" to Opcodes.ACC_PUBLIC,
                    LifecycleAccessorEmitter.VISIT_METHOD + LifecycleAccessorEmitter.VISIT_DESCRIPTOR to Opcodes.ACC_PUBLIC,
                ),
                emptyMap(),
            ),
            sink to HookAbiClass(
                Opcodes.ACC_PUBLIC or Opcodes.ACC_INTERFACE, "java/lang/Object", emptyList(),
                mapOf("accept(Ljava/lang/Object;Ljava/lang/String;)V" to Opcodes.ACC_PUBLIC),
                emptyMap(),
            ),
            hooks to HookAbiClass(
                Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(),
                mapOf(
                    "watchLifecycleObject(Ljava/lang/Object;Ljava/lang/String;Ljava/lang/String;)V" to
                        (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC),
                    "watchLifecycleObject(Ljava/lang/Object;ILjava/lang/String;Ljava/lang/String;)V" to
                        (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC),
                ),
                emptyMap(),
            ),
        ))
    }

    @Test fun validateSdkRejectsMissingWatchLifecycleObject() {
        assertThrows(IllegalStateException::class.java) {
            LifecycleRuntimeAbi.validateSdk(mapOf(
                "io/jankhunter/runtime/JankHunterHooks" to HookAbiClass(
                    Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(), emptyMap(), emptyMap(),
                ),
            ))
        }
    }
}
