package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

class AndroidComponentAutoInitInstrumentationTest {
    @Test
    fun existingApplicationOnCreateBootstrapsCurrentProcess() {
        val output = instrument(
            componentFixture(
                className = "example/App",
                superName = "android/app/Application",
                methodName = "onCreate",
                methodDescriptor = "()V",
            ),
            className = "example/App",
            hierarchy = setOf("example/App", "android/app/Application"),
        )

        assertEquals(1, countCalls(output, "onCreate", "io/jankhunter/runtime/JankHunter", "autoInit"))
    }

    @Test
    fun missingServiceOnCreateGetsSyntheticBootstrap() {
        val output = instrument(
            componentFixture("example/SyncService", "android/app/Service"),
            className = "example/SyncService",
            hierarchy = setOf("example/SyncService", "android/app/Service"),
        )

        assertTrue(hasMethod(output, "onCreate", "()V"))
        assertEquals(1, countCalls(output, "onCreate", "io/jankhunter/runtime/JankHunter", "autoInit"))
        assertEquals(1, countCalls(output, "onCreate", "android/app/Service", "onCreate"))
    }

    @Test
    fun receiverOnReceiveUsesNoThrowBootstrap() {
        val output = instrument(
            componentFixture(
                className = "example/BootReceiver",
                superName = "android/content/BroadcastReceiver",
                methodName = "onReceive",
                methodDescriptor = "(Landroid/content/Context;Landroid/content/Intent;)V",
            ),
            className = "example/BootReceiver",
            hierarchy = setOf("example/BootReceiver", "android/content/BroadcastReceiver"),
        )

        assertEquals(1, countCalls(output, "onReceive", "io/jankhunter/runtime/JankHunter", "autoInit"))
    }

    @Test
    fun disabledAutoInitDoesNotChangeComponentLifecycle() {
        val source = componentFixture(
            className = "example/App",
            superName = "android/app/Application",
            methodName = "onCreate",
            methodDescriptor = "()V",
        )
        val output = instrument(
            source,
            className = "example/App",
            hierarchy = setOf("example/App", "android/app/Application"),
            autoInit = false,
        )

        assertEquals(0, countCalls(output, "onCreate", "io/jankhunter/runtime/JankHunter", "autoInit"))
    }

    private fun instrument(
        source: ByteArray,
        className: String,
        hierarchy: Set<String>,
        autoInit: Boolean = true,
    ): ByteArray {
        val reader = ClassReader(source)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                className,
                HookConfig(
                    autoInit = autoInit,
                    methodCounters = false,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = false,
                    flowInteractions = false,
                    logSpam = false,
                    classGraph = false,
                    runtimeCallGraph = false,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = "",
                    ownerMapEntriesDirectory = "",
                ),
                classHierarchy = hierarchy,
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun componentFixture(
        className: String,
        superName: String,
        methodName: String? = null,
        methodDescriptor: String = "()V",
    ): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, className, null, superName, null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "()V", null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, superName, "<init>", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        if (methodName != null) {
            writer.visitMethod(Opcodes.ACC_PUBLIC, methodName, methodDescriptor, null, null).apply {
                visitCode()
                visitInsn(if (methodDescriptor.endsWith(")Z")) Opcodes.ICONST_1 else Opcodes.NOP)
                visitInsn(if (methodDescriptor.endsWith(")Z")) Opcodes.IRETURN else Opcodes.RETURN)
                visitMaxs(0, 0)
                visitEnd()
            }
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun hasMethod(bytes: ByteArray, targetName: String, targetDescriptor: String): Boolean {
        var found = false
        ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(
                access: Int,
                name: String,
                descriptor: String,
                signature: String?,
                exceptions: Array<out String>?,
            ): MethodVisitor? {
                if (name == targetName && descriptor == targetDescriptor) found = true
                return null
            }
        }, ClassReader.SKIP_CODE)
        return found
    }

    private fun countCalls(bytes: ByteArray, targetMethod: String, owner: String, callName: String): Int {
        var count = 0
        ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitMethod(
                access: Int,
                name: String,
                descriptor: String,
                signature: String?,
                exceptions: Array<out String>?,
            ): MethodVisitor? {
                if (name != targetMethod) return null
                return object : MethodVisitor(Opcodes.ASM9) {
                    override fun visitMethodInsn(
                        opcodeAndSource: Int,
                        callOwner: String,
                        methodName: String,
                        callDescriptor: String,
                        isInterface: Boolean,
                    ) {
                        if (callOwner == owner && methodName == callName) count += 1
                    }
                }
            }
        }, 0)
        return count
    }
}
