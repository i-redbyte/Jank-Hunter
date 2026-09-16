package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.Opcodes
import java.io.File
import java.io.IOException
import java.nio.file.Files

class LambdaCaptureWriterTest {
    @Test
    fun writesDeterministicEscapedShardAndDeletesStaleEmptyShard() {
        val directory = Files.createTempDirectory("jankhunter-lambda-captures").toFile()
        val capture = LambdaCapture(
            callsiteId = "stable:0x0000000000000001",
            owner = "com.app.Screen.render()V",
            implementation = "com.app.Screen.render\$lambda\$0",
            functionalInterface = "kotlin.jvm.functions.Function0",
            representation = "invokedynamic",
            values = listOf(LambdaCapturedValue("com.app.\"Owner", "capture", "strong")),
            sinks = listOf("compose.remember"),
            line = 42,
        )

        LambdaCaptureWriter.write(directory.absolutePath, "com/app/Screen", listOf(capture))

        val lines = InstrumentationArtifactFiles.readJsonlLines(directory)
        assertEquals(1, lines.size)
        assertTrue(lines.single().contains("\"format\":${ArtifactSchemas.LAMBDA_CAPTURE_FORMAT}"))
        assertTrue(lines.single().contains("com.app.\\\"Owner"))
        assertTrue(lines.single().contains("\"line\":42"))

        LambdaCaptureWriter.write(directory.absolutePath, "com/app/Screen", emptyList())

        assertTrue(InstrumentationArtifactFiles.readJsonlLines(directory).isEmpty())
        assertFalse(directory.walkTopDown().any { it.isFile && it.extension == "tmp" })
    }

    @Test
    fun atomicWriterRemovesTemporaryFileAfterFatalFailure() {
        val directory = Files.createTempDirectory("jankhunter-artifact-fatal").toFile()

        assertThrows(AssertionError::class.java) {
            InstrumentationArtifactFiles.writeClassShard(directory.absolutePath, "com/app/Broken") { writer ->
                writer.write("partial")
                throw AssertionError("synthetic fatal failure")
            }
        }

        assertFalse(directory.walkTopDown().any { it.isFile && it.extension == "tmp" })
        assertTrue(InstrumentationArtifactFiles.readJsonlLines(directory).isEmpty())
    }

    @Test
    fun writerChunksLargeClassesIntoBoundedJsonlRecords() {
        val directory = Files.createTempDirectory("jankhunter-lambda-captures-chunks").toFile()
        val captures = List(LAMBDA_CAPTURES_PER_RECORD + 1) { index ->
            LambdaCapture(
                callsiteId = "stable:0x${(index + 1).toString(16).padStart(16, '0')}",
                owner = "com.app.Screen.render()V",
                implementation = "com.app.Screen.render\$lambda\$$index",
                functionalInterface = "kotlin.jvm.functions.Function0",
                representation = "invokedynamic",
                values = listOf(LambdaCapturedValue("android.app.Activity", "capture", "strong")),
                sinks = emptyList(),
            )
        }

        LambdaCaptureWriter.write(directory.absolutePath, "com/app/Screen", captures)

        val lines = InstrumentationArtifactFiles.readJsonlLines(directory)
        assertEquals(2, lines.size)
        assertEquals(LAMBDA_CAPTURES_PER_RECORD, lines.first().countOccurrences("\"callsiteId\""))
        assertEquals(1, lines.last().countOccurrences("\"callsiteId\""))
    }

    @Test
    fun writerRepairsMissingSuppressionReasonAtArtifactBoundary() {
        val directory = Files.createTempDirectory("jankhunter-lambda-captures-suppression").toFile()
        val capture = LambdaCapture(
            callsiteId = "stable:0x0000000000000001",
            owner = "com.app.Screen.render()V",
            implementation = "com.app.Screen.render\$lambda\$0",
            functionalInterface = "kotlin.jvm.functions.Function0",
            representation = "invokedynamic",
            values = listOf(LambdaCapturedValue("android.app.Activity", "capture", "strong")),
            sinks = emptyList(),
            suppressed = true,
        )

        LambdaCaptureWriter.write(directory.absolutePath, "com/app/Screen", listOf(capture))

        assertTrue(InstrumentationArtifactFiles.readJsonlLines(directory).single().contains("\"suppressionReason\":\"reason not provided\""))
    }

    @Test
    fun emptyCaptureFailsLoudlyWhenStaleShardCannotBeDeleted() {
        val directory = Files.createTempDirectory("jankhunter-lambda-captures-stale").toFile()
        val capture = LambdaCapture(
            callsiteId = "stable:0x0000000000000001",
            owner = "com.app.Screen.render()V",
            implementation = "com.app.Screen.render\$lambda\$0",
            functionalInterface = "kotlin.jvm.functions.Function0",
            representation = "invokedynamic",
            values = listOf(LambdaCapturedValue("android.app.Activity", "capture", "strong")),
            sinks = emptyList(),
        )
        LambdaCaptureWriter.write(directory.absolutePath, "com/app/Screen", listOf(capture))
        val shard = directory.walkTopDown().single { it.isFile && it.extension == "jsonl" }
        assertTrue(shard.delete())
        assertTrue(shard.mkdir())
        File(shard, "blocker").writeText("keep directory non-empty")

        assertThrows(IOException::class.java) {
            LambdaCaptureWriter.write(directory.absolutePath, "com/app/Screen", emptyList())
        }
    }

    @Test
    fun classBuilderDoesNotRetainClassOrMethodTrees() {
        val builder = LambdaCaptureClassBuilder("com/app/Screen")
        builder.recordClass(Opcodes.ACC_PUBLIC, "com/app/Screen", "java/lang/Object", null)
        builder.recordField(Opcodes.ACC_PRIVATE, "title", "Ljava/lang/String;")

        assertFalse(builder.javaClass.declaredFields.any { org.objectweb.asm.tree.ClassNode::class.java.isAssignableFrom(it.type) })
        assertFalse(builder.javaClass.declaredFields.any { org.objectweb.asm.tree.MethodNode::class.java.isAssignableFrom(it.type) })
    }

    @Test
    fun unrelatedAnnotationIsDelegatedExactlyOnce() {
        var visits = 0
        val downstream = object : ClassVisitor(Opcodes.ASM9) {
            override fun visitAnnotation(descriptor: String?, visible: Boolean): AnnotationVisitor? {
                visits++
                return null
            }
        }
        val visitor = JankHunterClassVisitor(
            downstream,
            "com/app/Screen",
            HookConfig(
                methodCounters = false,
                okhttp = false,
                webSockets = false,
                handlers = false,
                executors = false,
                coroutines = false,
                interactionOperations = false,
                logSpam = false,
                classGraph = true,
                runtimeCallGraph = false,
                classGraphDirectory = "",
                instrumentationDiagnosticsDirectory = "",
                databaseTracing = false,
            ),
        )

        visitor.visitAnnotation("Lcom/app/Marker;", false)

        assertEquals(1, visits)
    }

    private fun String.countOccurrences(value: String): Int {
        var count = 0
        var start = 0
        while (true) {
            val index = indexOf(value, start)
            if (index < 0) return count
            count++
            start = index + value.length
        }
    }
}
