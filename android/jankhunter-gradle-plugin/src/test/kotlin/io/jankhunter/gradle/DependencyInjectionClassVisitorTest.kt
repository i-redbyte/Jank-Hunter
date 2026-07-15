package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import java.nio.file.Files

class DependencyInjectionClassVisitorTest {
    @Test
    fun recordsDeclaredInjectConstructorAndField() {
        val directory = Files.createTempDirectory("jankhunter-di-declared").toFile()
        scan(
            declaredInjectionFixture(),
            "com/app/FeedViewModel",
            directory.absolutePath,
        )

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        assertTrue(text.contains("\"kind\":\"class\""))
        assertTrue(text.contains("\"roles\":[\"consumer\"]"))
        assertTrue(text.contains("\"consumer\":\"com.app.FeedViewModel\""))
        assertTrue(text.contains("\"dependency\":\"com.app.FeedRepository\""))
        assertTrue(text.contains("\"injectionKind\":\"constructor\""))
        assertTrue(text.contains("\"dependency\":\"com.app.Analytics\""))
        assertTrue(text.contains("\"injectionKind\":\"field\""))
        assertTrue(text.contains("\"dependency\":\"com.app.Scheduler\""))
        assertTrue(text.contains("\"injectionKind\":\"method\""))
    }

    @Test
    fun recordsProvidesAndBindsDependencies() {
        val directory = Files.createTempDirectory("jankhunter-di-module").toFile()
        scan(daggerModuleFixture(), "com/app/DataModule", directory.absolutePath)

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        assertTrue(text.contains("\"roles\":[\"module\"]"))
        assertTrue(text.contains("\"consumer\":\"com.app.FeedRepository\""))
        assertTrue(text.contains("\"dependency\":\"com.app.Api\""))
        assertTrue(text.contains("\"injectionKind\":\"provider\""))
        assertTrue(text.contains("\"consumer\":\"com.app.Repository\""))
        assertTrue(text.contains("\"dependency\":\"com.app.RepositoryImpl\""))
        assertTrue(text.contains("\"injectionKind\":\"binding\""))
    }

    @Test
    fun generatedFactoryIsScannedWithoutJankHunterMarkerOrHooks() {
        val directory = Files.createTempDirectory("jankhunter-di-generated").toFile()
        val output = scan(
            generatedFactoryFixture(),
            "com/app/FeedViewModel_Factory",
            directory.absolutePath,
            generated = true,
            framework = DependencyInjectionFramework.DAGGER2,
        )

        val annotations = linkedSetOf<String>()
        val hookOwners = linkedSetOf<String>()
        ClassReader(output).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitAnnotation(descriptor: String, visible: Boolean) =
                    super.visitAnnotation(descriptor, visible).also { annotations += descriptor }

                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ) = object : org.objectweb.asm.MethodVisitor(Opcodes.ASM9) {
                    override fun visitMethodInsn(
                        opcode: Int,
                        owner: String,
                        methodName: String,
                        methodDescriptor: String,
                        isInterface: Boolean,
                    ) {
                        hookOwners += owner
                    }
                }
            },
            0,
        )
        assertFalse(annotations.contains(InstrumentationMarker.DESCRIPTOR))
        assertFalse(hookOwners.any { it.startsWith("io/jankhunter/") })

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        assertTrue(text.contains("\"generated\":true"))
        assertTrue(text.contains("\"injectionKind\":\"generated_factory\""))
        assertTrue(text.contains("\"resolution\":\"generated_confirmed\""))
    }

    @Test
    fun recordsHiltInstallInComponentMetadata() {
        val directory = Files.createTempDirectory("jankhunter-di-hilt").toFile()
        scan(hiltModuleFixture(), "com/app/NetworkModule", directory.absolutePath)

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        assertTrue(text.contains("\"framework\":\"hilt\""))
        assertTrue(text.contains("\"roles\":[\"module\"]"))
        assertTrue(text.contains("dagger.hilt.components.SingletonComponent"))
    }

    @Test
    fun recordsGeneratedMembersInjectorDependency() {
        val directory = Files.createTempDirectory("jankhunter-di-members-injector").toFile()
        scan(
            membersInjectorFixture(),
            "com/app/FeedFragment_MembersInjector",
            directory.absolutePath,
            generated = true,
            framework = DependencyInjectionFramework.DAGGER2,
        )

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        assertTrue(text.contains("\"roles\":[\"members_injector\"]"))
        assertTrue(text.contains("\"consumer\":\"com.app.FeedFragment\""))
        assertTrue(text.contains("\"dependency\":\"com.app.Analytics\""))
        assertTrue(text.contains("\"injectionKind\":\"members_injector\""))
        assertTrue(text.contains("\"resolution\":\"generated_confirmed\""))
    }

    @Test
    fun recordsKoinAnnotationWithoutInterpretingRuntimeDsl() {
        val directory = Files.createTempDirectory("jankhunter-di-koin").toFile()
        scan(koinDefinitionFixture(), "com/app/FeedPresenter", directory.absolutePath)

        val text = InstrumentationArtifactFiles.readJsonlLines(directory).joinToString("\n")
        val compactText = text.filterNot(Char::isWhitespace)
        assertTrue(text.contains("\"framework\":\"koin\""))
        assertTrue(compactText.contains("\"roles\":[\"binding\",\"consumer\"]"))
        assertTrue(text.contains("\"dependency\":\"com.app.FeedRepository\""))
        assertTrue(text.contains("\"injectionKind\":\"koin_definition\""))
    }

    @Test
    fun identifiesGeneratedDiClassesOutsideTheApplicationIncludeBoundary() {
        assertTrue(
            DependencyInjectionClassMatcher.isGeneratedDiClass(
                "com.other.FeedViewModel_Factory",
                listOf("dagger.internal.Factory"),
            ),
        )
        assertTrue(DependencyInjectionClassMatcher.isGeneratedDiClass("org.koin.ksp.generated.DefaultModule"))
        assertTrue(DependencyInjectionClassMatcher.isGeneratedDiClass("hilt_aggregated_deps._com_app_AppModule"))
        assertFalse(DependencyInjectionClassMatcher.isGeneratedDiClass("com.app.RealFactory"))
    }

    private fun scan(
        bytes: ByteArray,
        className: String,
        directory: String,
        generated: Boolean = false,
        framework: DependencyInjectionFramework? = null,
    ): ByteArray {
        val reader = ClassReader(bytes)
        val writer = ClassWriter(reader, 0)
        reader.accept(
            DependencyInjectionClassVisitor(
                writer,
                className,
                directory,
                generated,
                framework,
            ),
            0,
        )
        return writer.toByteArray()
    }

    private fun declaredInjectionFixture(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "com/app/FeedViewModel", null, "java/lang/Object", null)
        writer.visitField(Opcodes.ACC_PRIVATE, "analytics", "Lcom/app/Analytics;", null, null).apply {
            visitAnnotation("Ljavax/inject/Inject;", true).visitEnd()
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "(Lcom/app/FeedRepository;)V", null, null).apply {
            visitAnnotation("Ljavax/inject/Inject;", true).visitEnd()
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(1, 2)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC, "setScheduler", "(Lcom/app/Scheduler;)V", null, null).apply {
            visitAnnotation("Ljavax/inject/Inject;", true).visitEnd()
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 2)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun daggerModuleFixture(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(
            Opcodes.V17,
            Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT,
            "com/app/DataModule",
            null,
            "java/lang/Object",
            null,
        )
        writer.visitAnnotation("Ldagger/Module;", true).visitEnd()
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "provideRepository",
            "(Lcom/app/Api;)Lcom/app/FeedRepository;",
            null,
            null,
        ).apply {
            visitAnnotation("Ldagger/Provides;", true).visitEnd()
            visitCode()
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(1, 1)
            visitEnd()
        }
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT,
            "bindRepository",
            "(Lcom/app/RepositoryImpl;)Lcom/app/Repository;",
            null,
            null,
        ).apply {
            visitAnnotation("Ldagger/Binds;", true).visitEnd()
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun generatedFactoryFixture(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(
            Opcodes.V17,
            Opcodes.ACC_PUBLIC,
            "com/app/FeedViewModel_Factory",
            null,
            "java/lang/Object",
            arrayOf("dagger/internal/Factory"),
        )
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "newInstance",
            "(Lcom/app/FeedRepository;)Lcom/app/FeedViewModel;",
            null,
            null,
        ).apply {
            visitCode()
            visitInsn(Opcodes.ACONST_NULL)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(1, 1)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun membersInjectorFixture(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(
            Opcodes.V17,
            Opcodes.ACC_PUBLIC,
            "com/app/FeedFragment_MembersInjector",
            null,
            "java/lang/Object",
            arrayOf("dagger/MembersInjector"),
        )
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "injectAnalytics",
            "(Lcom/app/FeedFragment;Lcom/app/Analytics;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 2)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun koinDefinitionFixture(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "com/app/FeedPresenter", null, "java/lang/Object", null)
        writer.visitAnnotation("Lorg/koin/core/annotation/Single;", true).visitEnd()
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "(Lcom/app/FeedRepository;)V", null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitInsn(Opcodes.RETURN)
            visitMaxs(1, 2)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun hiltModuleFixture(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "com/app/NetworkModule", null, "java/lang/Object", null)
        writer.visitAnnotation("Ldagger/Module;", true).visitEnd()
        writer.visitAnnotation("Ldagger/hilt/InstallIn;", true).apply {
            visitArray("value").apply {
                visit(null, Type.getObjectType("dagger/hilt/components/SingletonComponent"))
                visitEnd()
            }
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }
}
