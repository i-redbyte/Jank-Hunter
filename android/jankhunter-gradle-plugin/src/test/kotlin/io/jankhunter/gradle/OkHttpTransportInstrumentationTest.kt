package io.jankhunter.gradle

import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import com.android.build.api.instrumentation.InstrumentationContext
import org.gradle.testfixtures.ProjectBuilder
import java.util.jar.JarEntry
import java.util.jar.JarOutputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.analysis.Analyzer
import org.objectweb.asm.tree.analysis.BasicVerifier

class OkHttpTransportInstrumentationTest {
    @Test
    fun externalDependencyUsesApplicationClasspathForOptionalTransportAbi() {
        val project = ProjectBuilder.builder().build()
        val parameters = project.objects.newInstance(OkHttpTransportInstrumentationParameters::class.java)
        parameters.excludePackages.set(emptySet())
        val factory = object : OkHttpTransportClassVisitorFactory() {
            override val parameters = project.objects.property(OkHttpTransportInstrumentationParameters::class.java)
                .value(parameters)
            override val instrumentationContext: InstrumentationContext get() = error("not used")
        }
        val context = object : ClassContext {
            override val currentClassData = ClassInfo("okhttp3.internal.connection.RealConnection")
            // AGP scopes this resolver to OkHttp's own dependencies, not its consumer's helper.
            override fun loadClassData(className: String): ClassData? =
                if (className == "okhttp3.internal.http1.Http1Codec") ClassInfo(className) else null
        }
        val helper = project.layout.projectDirectory.file("helper.jar").asFile
        JarOutputStream(helper.outputStream()).use {
            it.putNextEntry(JarEntry(ABI.replace('.', '/') + ".class"))
            it.write(ownerInterface())
            it.closeEntry()
        }
        parameters.supportClasspath.from(helper)
        val writer = ClassWriter(ClassWriter.COMPUTE_MAXS)
        ClassReader(original(context.currentClassData.className)).accept(factory.createClassVisitor(context, writer), 0)
        val node = ClassNode()
        ClassReader(writer.toByteArray()).accept(node, 0)
        assertTrue("available helper in application classpath must enable dependency hooks", ABI.replace('.', '/') in node.interfaces)

        parameters.supportClasspath.setFrom(emptyList<Any>())
        assertSame("older/missing helper must not receive unresolved ABI calls", writer, factory.createClassVisitor(context, writer))
    }

    @Test
    fun transportClassesHaveValidStacksAndInstrumentationIsIdempotent() {
        for (name in NAMES) {
            val transformed = transform(original(name))
            val node = ClassNode()
            ClassReader(transformed).accept(node, 0)
            assertTrue("no transport hook emitted for $name", node.methods.any { method ->
                method.instructions.toArray().any { instruction ->
                    instruction is org.objectweb.asm.tree.MethodInsnNode && instruction.owner == OkHttpTransportClassVisitor.HELPER
                }
            })
            for (method in node.methods) Analyzer(BasicVerifier()).analyze(node.name, method)
            assertArrayEquals("second transform changed $name", transformed, transform(transformed))
        }
    }

    @Test
    fun injectedConnectionStateCanBeStoredAndReadWithoutChangingItsIdentity() {
        val name = "okhttp3.internal.connection.RealConnection"
        val definitions = mapOf(name to transform(original(name)), ABI to ownerInterface())
        val loader = object : ClassLoader(javaClass.classLoader) {
            override fun loadClass(name: String, resolve: Boolean): Class<*> = synchronized(getClassLoadingLock(name)) {
                val bytes = definitions[name] ?: return@synchronized super.loadClass(name, resolve)
                val type = findLoadedClass(name) ?: defineClass(name, bytes, 0, bytes.size)
                if (resolve) resolveClass(type)
                type
            }
        }
        val connectionType = loader.loadClass(name)
        val connection = connectionType.declaredConstructors.single().newInstance(null, null)
        val state = Any()
        connectionType.getMethod("jankHunterTransportState", Any::class.java).invoke(connection, state)
        assertSame(state, connectionType.getMethod("jankHunterTransportState").invoke(connection))
    }

    @Test
    fun unsupportedConnectionLayoutIsNotPartiallyRewritten() {
        val source = ClassNode()
        ClassReader(original("okhttp3.internal.connection.RealConnection")).accept(source, 0)
        source.methods.removeAll { it.name == "startHttp2" }
        val writer = ClassWriter(0)
        source.accept(writer)
        assertArrayEquals(writer.toByteArray(), transform(writer.toByteArray()))
    }

    private fun original(name: String): ByteArray = checkNotNull(javaClass.classLoader
        .getResourceAsStream(name.replace('.', '/') + ".class")).use { it.readBytes() }

    private fun transform(bytes: ByteArray): ByteArray {
        val writer = ClassWriter(ClassReader(bytes), ClassWriter.COMPUTE_MAXS)
        ClassReader(bytes).accept(OkHttpTransportClassVisitor(writer), 0)
        return writer.toByteArray()
    }

    private fun ownerInterface(): ByteArray {
        val writer = ClassWriter(0)
        writer.visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC or Opcodes.ACC_INTERFACE or Opcodes.ACC_ABSTRACT,
            ABI.replace('.', '/'), null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT, "jankHunterTransportState", "()Ljava/lang/Object;", null, null).visitEnd()
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_ABSTRACT, "jankHunterTransportState", "(Ljava/lang/Object;)V", null, null).visitEnd()
        writer.visitEnd()
        return writer.toByteArray()
    }

    private companion object {
        const val ABI = "io.jankhunter.okhttp3.JankHunterHttpTransportV1"
        val NAMES = listOf("okhttp3.internal.connection.RealConnection", "okhttp3.internal.http2.Http2Codec",
            "okhttp3.internal.http2.Http2Connection", "okhttp3.RealCall", "okhttp3.Cache\$CacheResponseBody")
    }

    private data class ClassInfo(
        override val className: String,
        override val classAnnotations: List<String> = emptyList(),
        override val interfaces: List<String> = emptyList(),
        override val superClasses: List<String> = emptyList(),
    ) : ClassData
}
