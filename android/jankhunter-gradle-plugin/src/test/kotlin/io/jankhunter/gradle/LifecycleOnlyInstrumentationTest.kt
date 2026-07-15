package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import java.nio.file.Files

class LifecycleOnlyInstrumentationTest {
    @Test
    fun dependencyFragmentGetsLifecycleWatchWithoutHighVolumeHooks() {
        val diagnostics = Files.createTempDirectory("jankhunter-lifecycle-diagnostics").toFile()

        val instrumented = instrument(fixture(), diagnostics.absolutePath)
        val calls = collectCalls(instrumented)

        assertEquals(1, calls.count { it.method == "watchLifecycleObject" })
        assertFalse(calls.any { it.method == "recordMethodCall" })
        assertFalse(calls.any { it.method == "enterMethod" || it.method == "exitMethod" })
        assertEquals(1, countClassAnnotation(instrumented, LifecycleInstrumentationMarker.DESCRIPTOR))
        assertTrue(
            InstrumentationArtifactFiles.readJsonlLines(diagnostics)
                .joinToString("\n")
                .contains("\"intent\":\"lifecycle.watch_retained\""),
        )
    }

    @Test
    fun lifecycleMarkerMakesDependencyPassIdempotent() {
        val once = instrument(fixture())
        val twice = instrument(once)

        assertEquals(1, collectCalls(twice).count { it.method == "watchLifecycleObject" })
        assertEquals(1, countClassAnnotation(twice, LifecycleInstrumentationMarker.DESCRIPTOR))
    }

    @Test
    fun classWithoutLifecycleHookGetsNoMarkerOrDiagnosticsShard() {
        val diagnostics = Files.createTempDirectory("jankhunter-no-lifecycle-diagnostics").toFile()
        val instrumented = instrument(fixture(includeLifecycleMethod = false), diagnostics.absolutePath)

        assertEquals(0, countClassAnnotation(instrumented, LifecycleInstrumentationMarker.DESCRIPTOR))
        assertTrue(InstrumentationArtifactFiles.readJsonlLines(diagnostics).isEmpty())
    }

    private fun instrument(bytes: ByteArray, diagnosticsDirectory: String = ""): ByteArray {
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                next = writer,
                className = CLASS_NAME,
                config = HookConfig(
                    embeddedSymbols = false,
                    methodCounters = false,
                    okhttp = false,
                    webSockets = false,
                    handlers = false,
                    executors = false,
                    coroutines = false,
                    flowInteractions = false,
                    lifecycleLeaks = true,
                    logSpam = false,
                    classGraph = false,
                    runtimeCallGraph = false,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = diagnosticsDirectory,
                    ownerMapEntriesDirectory = "",
                ),
                classHierarchy = setOf(CLASS_NAME, "androidx/fragment/app/Fragment"),
                instrumentationMarkerDescriptor = LifecycleInstrumentationMarker.DESCRIPTOR,
                markerOnlyWhenHookApplied = true,
                diagnosticsOnlyWhenHookApplied = true,
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun fixture(includeLifecycleMethod: Boolean = true): ByteArray {
        val writer = ClassWriter(ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(
            Opcodes.V17,
            Opcodes.ACC_PUBLIC,
            CLASS_NAME,
            null,
            "androidx/fragment/app/Fragment",
            null,
        )
        if (includeLifecycleMethod) {
            writer.visitMethod(Opcodes.ACC_PUBLIC, "onDestroyView", "()V", null, null).apply {
                visitCode()
                visitInsn(Opcodes.RETURN)
                visitMaxs(0, 0)
                visitEnd()
            }
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC, "render", "()V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun collectCalls(bytes: ByteArray): List<Call> {
        val calls = mutableListOf<Call>()
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor {
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            methodDescriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (owner == "io/jankhunter/runtime/JankHunterHooks") {
                                calls += Call(methodName, methodDescriptor)
                            }
                        }
                    }
                }
            },
            0,
        )
        return calls
    }

    private fun countClassAnnotation(bytes: ByteArray, targetDescriptor: String): Int {
        var count = 0
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitAnnotation(descriptor: String, visible: Boolean) =
                    super.visitAnnotation(descriptor, visible).also {
                        if (descriptor == targetDescriptor) count++
                    }
            },
            ClassReader.SKIP_CODE or ClassReader.SKIP_DEBUG or ClassReader.SKIP_FRAMES,
        )
        return count
    }

    private data class Call(val method: String, val descriptor: String)

    private companion object {
        const val CLASS_NAME = "ru/mail/im/base/ui/fragment/BaseFragment"
    }
}
