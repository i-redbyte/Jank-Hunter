package io.jankhunter.gradle

import org.junit.Assert.assertThrows
import org.junit.Test
import org.gradle.testfixtures.ProjectBuilder
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class ValidateJankHunterHookAbiTaskTest {
    private val owner = "io/jankhunter/runtime/JankHunterHooks"

    @Test fun compileOnlyLibraryValidatesAvailableHeadersAndDefersRuntimeToApplication() {
        val root = kotlin.io.path.createTempDirectory("compile-only-sdk").toFile()
        try {
            val classes = root.resolve("sdk")
            val file = classes.resolve("$owner.class").apply { parentFile.mkdirs() }
            file.writeBytes(ClassWriter(0).apply {
                visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC, owner, null, "java/lang/Object", null)
                visitEnd()
            }.toByteArray())
            val project = ProjectBuilder.builder().withProjectDir(root.resolve("project")).build()
            val task = project.tasks.register("validateAbi", ValidateJankHunterHookAbiTask::class.java).get()
            task.requiredOwners.set(setOf(owner))
            task.compileSdk.from(classes)
            task.requireSdk.set(false)
            val output = root.resolve("abi.bin")
            task.outputFile.set(output)
            task.validate()
            val index = HookAbiIndex.read(output)
            assertThrows(IllegalStateException::class.java) {
                index.validateMethod(Opcodes.INVOKESTATIC, owner, "missing", "()V")
            }
            task.requireSdk.set(true)
            assertThrows(IllegalStateException::class.java) { task.validate() }
        } finally { root.deleteRecursively() }
    }

    @Test fun missingMethodAndStaticMismatchFailBeforeHookIsAccepted() {
        val header = HookAbiClass(Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(),
            mapOf("hook()V" to Opcodes.ACC_PUBLIC), emptyMap())
        val index = HookAbiIndex(mapOf(owner to header), deferred = false)
        assertThrows(IllegalStateException::class.java) { index.validateMethod(Opcodes.INVOKESTATIC, owner, "hook", "()V") }
        assertThrows(IllegalStateException::class.java) { index.validateMethod(Opcodes.INVOKESTATIC, owner, "missing", "()V") }
        index.validateMethod(Opcodes.INVOKEVIRTUAL, owner, "hook", "()V")
    }

    @Test fun requiredRuntimeSurfaceCannotDropCompiledSymbols() {
        val compile = HookAbiClass(Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(),
            mapOf("hook()V" to (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC)), emptyMap())
        val runtime = compile.copy(methods = emptyMap())
        assertThrows(IllegalStateException::class.java) {
            HookAbiIndex.requireRuntimeSurface(mapOf(owner to compile), mapOf(owner to runtime))
        }
        HookAbiIndex.requireRuntimeSurface(mapOf(owner to compile), mapOf(owner to compile))
    }

    @Test fun compatibleOldSdkNeedsNoNewProtocolFieldAndObjectMethodsResolve() {
        val header = HookAbiClass(Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(),
            mapOf("hook()V" to (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC)), emptyMap())
        val index = HookAbiIndex(mapOf(owner to header), deferred = false)
        index.validateMethod(Opcodes.INVOKESTATIC, owner, "hook", "()V")
        index.validateMethod(Opcodes.INVOKEVIRTUAL, owner, "toString", "()Ljava/lang/String;")
    }

    @Test fun librariesWithoutRuntimeCanDeferToApplicationValidation() {
        HookAbiIndex(emptyMap(), deferred = true).validateMethod(Opcodes.INVOKESTATIC, owner, "hook", "()V")
        assertThrows(IllegalStateException::class.java) {
            HookAbiIndex(emptyMap(), deferred = false).validateMethod(Opcodes.INVOKESTATIC, owner, "hook", "()V")
        }
    }

    @Test fun disabledOkHttpOwnersAreNotValidatedAtRuntime() {
        val header = HookAbiClass(Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(),
            mapOf("hook()V" to (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC)), emptyMap())
        val index = HookAbiIndex(
            mapOf(owner to header),
            deferred = false,
            requiredOwners = setOf(owner),
        )
        index.validateMethod(Opcodes.INVOKESTATIC, "io/jankhunter/okhttp3/JankHunterOkHttp3", "install", "()V")
    }

    @Test fun hookAbiReportRoundTripsRequiredOwners() {
        val file = kotlin.io.path.createTempFile("hook-abi", ".bin").toFile()
        val header = HookAbiClass(Opcodes.ACC_PUBLIC, "java/lang/Object", emptyList(),
            mapOf("hook()V" to (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC)), emptyMap())
        try {
            HookAbiIndex(mapOf(owner to header), deferred = false, requiredOwners = setOf(owner)).write(file)
            val restored = HookAbiIndex.read(file)
            restored.validateMethod(Opcodes.INVOKESTATIC, owner, "hook", "()V")
            assertThrows(IllegalStateException::class.java) {
                restored.validateMethod(Opcodes.INVOKESTATIC, owner, "missing", "()V")
            }
        } finally {
            file.delete()
        }
    }
}
