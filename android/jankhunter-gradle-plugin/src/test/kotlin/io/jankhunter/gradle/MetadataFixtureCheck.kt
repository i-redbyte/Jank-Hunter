package io.jankhunter.gradle

import java.io.File
import java.io.PrintWriter
import java.io.StringWriter
import org.objectweb.asm.util.CheckClassAdapter
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode

/** Release fixture entrypoint: missing compiler output is an error, never a skipped test. */
object MetadataFixtureCheck {
    private fun fixtureHooks() = HookConfig(methodCounters = false, okhttp = false, webSockets = false,
        handlers = false, executors = false, coroutines = false, interactionOperations = false,
        logSpam = false, classGraph = false, runtimeCallGraph = true, classGraphDirectory = "",
        instrumentationDiagnosticsDirectory = "")

    @JvmStatic fun main(args: Array<String>) {
        check(args.size == 1) { "Expected the real compiler fixture root" }
        val root = File(args.single())
        for (target in listOf(8, 11, 17, 21)) {
            val source = File(root, "java-$target/fixture/javaabi/JavaTarget.class").readBytes()
            check(ClassReader(source).readUnsignedShort(6) == target + 44)
            val reader = ClassReader(source)
            val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
            reader.accept(JankHunterClassVisitor(writer, reader.className, fixtureHooks()), ClassReader.EXPAND_FRAMES)
            val transformed = writer.toByteArray()
            val diagnostics = StringWriter()
            CheckClassAdapter.verify(ClassReader(transformed), false, PrintWriter(diagnostics))
            check(diagnostics.toString().isEmpty()) { diagnostics.toString() }
            check(KotlinGeneratedMethodIndex.EMPTY.analysisStatus == KotlinMetadataAnalysisStatus.ABSENT)
            File(root, "java-$target/instrumented.class").writeBytes(transformed)
            println("Java $target real classfile; FULL transform and ASM verification PASS")
        }
        for (compiler in listOf("1.3.72", "1.4.32", "1.5.31", "1.6.21", "1.7.22", "1.8.22", "1.9.23", "2.0.21", "2.1.21", "2.2.21", "2.3.20")) {
            val compilerRoot = File(root, compiler)
            val expected = listOf(
                "fixture/metadata/MetadataData.class",
                "fixture/metadata/MetadataInline.class",
                "fixture/metadata/MetadataSuspend.class",
                "fixture/metadata/MetadataMultifile.class",
            ) + if (compiler in setOf("1.3.72", "1.4.32")) emptyList() else listOf("fixture/metadata/MetadataValue.class")
            expected.forEach { relative ->
                val fixture = File(compilerRoot, relative)
                check(fixture.isFile) { "Missing compiler fixture $compiler/$relative" }
            }
            val source = File(compilerRoot, "fixture/metadata/MetadataData.class").readBytes()
            val reader = ClassReader(source)
            var index = KotlinGeneratedMethodIndex.EMPTY
            reader.accept(object : ClassVisitor(Opcodes.ASM9) {
                override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? =
                    if (descriptor == KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR)
                        KotlinGeneratedMethodIndex.collectingVisitor(null) { index = it } else null
            }, ClassReader.SKIP_CODE)
            check(index.analysisStatus == KotlinMetadataAnalysisStatus.READABLE) { "Unreadable $compiler fixture" }
            check(index.reason("component1", "()I") == "kotlin_generated_function")
            check(index.origin("declared", "(I)I") == KotlinMethodOrigin.DECLARED)
            val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
            reader.accept(JankHunterClassVisitor(writer, reader.className, HookConfig(
                methodCounters = false, okhttp = false, webSockets = false, handlers = false,
                executors = false, coroutines = false, interactionOperations = false, logSpam = false,
                classGraph = false, runtimeCallGraph = true, classGraphDirectory = "",
                instrumentationDiagnosticsDirectory = "",
            )), ClassReader.EXPAND_FRAMES)
            val before = ClassNode().also { ClassReader(source).accept(it, 0) }
            val transformed = writer.toByteArray()
            val verification = StringWriter()
            CheckClassAdapter.verify(ClassReader(transformed), false, PrintWriter(verification))
            check(verification.toString().isEmpty()) { verification.toString() }
            val after = ClassNode().also { ClassReader(transformed).accept(it, 0) }
            fun metadata(node: ClassNode) = node.visibleAnnotations.single {
                it.desc == KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR
            }.values
            check(metadata(before) == metadata(after)) { "$compiler metadata changed during instrumentation" }
            File(root, "$compiler/instrumented.class").writeBytes(transformed)
            verifyReadableShape(compilerRoot, "MetadataInline")
            verifyReadableShape(compilerRoot, "MetadataSuspend")
            if (compiler !in setOf("1.3.72", "1.4.32")) {
                verifyReadableShape(compilerRoot, "MetadataValue")
            }
            verifyReadableShape(compilerRoot, "MetadataMultifile")
            verifyReadableShape(compilerRoot, "MetadataMultifile__MetadataMultifileKt")
            verifyReadableShape(compilerRoot, "MetadataMultifile__MetadataMultifilePartKt")
            println("$compiler readable; generated/declared classification and metadata preservation PASS")
        }
    }

    private fun verifyReadableShape(root: File, className: String) {
        val relative = "fixture/metadata/$className.class"
        val bytes = File(root, relative).readBytes()
        var index = KotlinGeneratedMethodIndex.EMPTY
        ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? =
                if (descriptor == KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR)
                    KotlinGeneratedMethodIndex.collectingVisitor(null) { index = it } else null
        }, ClassReader.SKIP_CODE)
        check(index.analysisStatus == KotlinMetadataAnalysisStatus.READABLE) { "Unreadable $relative" }
        when (className) {
            "MetadataInline" -> check(index.origin("compute", "(I)I") == KotlinMethodOrigin.DECLARED)
            "MetadataSuspend" -> check(index.origin("load", "(Lkotlin/coroutines/Continuation;)Ljava/lang/Object;").metadataKnown)
            "MetadataValue" -> check(index.origin("constructor-impl", "(I)I").metadataKnown)
            "MetadataMultifile" -> check(index.origin("multifilePartOne", "()I") == KotlinMethodOrigin.UNKNOWN)
            "MetadataMultifile__MetadataMultifileKt" -> check(index.origin("multifilePartOne", "()I") == KotlinMethodOrigin.DECLARED)
            "MetadataMultifile__MetadataMultifilePartKt" -> check(index.origin("multifilePartTwo", "()I") == KotlinMethodOrigin.DECLARED)
        }
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(JankHunterClassVisitor(writer, reader.className, fixtureHooks()), ClassReader.EXPAND_FRAMES)
        val transformed = writer.toByteArray()
        val diagnostics = StringWriter()
        CheckClassAdapter.verify(ClassReader(transformed), false, PrintWriter(diagnostics))
        check(diagnostics.toString().isEmpty()) { diagnostics.toString() }
        check(reader.className.endsWith(className)) { "Unexpected class for $relative" }
    }
}
