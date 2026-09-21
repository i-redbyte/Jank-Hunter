package io.jankhunter.gradle

import java.util.jar.JarEntry
import java.util.jar.JarOutputStream
import java.util.zip.ZipFile
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassReader
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.analysis.Analyzer
import org.objectweb.asm.tree.analysis.BasicVerifier

class InstrumentTransportTaskTest {
    @Test
    fun transformsRealOkHttpOnlyWithCapabilityAndPreservesVerifierFrames() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register("transport", InstrumentJankHunterTransportTask::class.java).get()
        val entry = "okhttp3/internal/connection/RealConnection.class"
        val bytes = requireNotNull(javaClass.classLoader.getResourceAsStream(entry)).use { it.readBytes() }
        val source = project.file("source.jar")
        JarOutputStream(source.outputStream()).use {
            it.putNextEntry(JarEntry(entry))
            it.write(bytes)
            it.closeEntry()
        }
        task.inputJars.set(listOf(project.layout.projectDirectory.file("source.jar")))
        task.inputDirectories.set(emptyList())
        task.excludePackages.set(emptySet())
        task.capabilityFile.set(project.layout.projectDirectory.file("capability.txt"))
        task.outputFile.set(project.layout.buildDirectory.file("transport.jar"))
        fun run(capability: TransportCapability): ByteArray {
            task.capabilityFile.get().asFile.writeText(capability.encoded)
            task.instrument()
            return ZipFile(task.outputFile.get().asFile).use { zip ->
                zip.getInputStream(zip.getEntry(entry)).use { it.readBytes() }
            }
        }
        assertArrayEquals(bytes, run(TransportCapability.MISSING_HELPER))
        assertArrayEquals(bytes, run(TransportCapability.UNSUPPORTED_CODEC))
        val transformed = run(TransportCapability.AVAILABLE)
        val node = ClassNode()
        ClassReader(transformed).accept(node, 0)
        assertTrue(OkHttpTransportClassVisitor.OWNER_ABI in node.interfaces)
        node.methods.forEach { Analyzer(BasicVerifier()).analyze(node.name, it) }
        assertArrayEquals(transformed, run(TransportCapability.AVAILABLE))
        val archive = task.outputFile.get().asFile.readBytes()
        run(TransportCapability.AVAILABLE)
        assertArrayEquals(archive, task.outputFile.get().asFile.readBytes())
        // A downstream app can receive classes already instrumented in a library.
        JarOutputStream(source.outputStream()).use {
            it.putNextEntry(JarEntry(entry))
            it.write(transformed)
            it.closeEntry()
        }
        assertArrayEquals(transformed, run(TransportCapability.AVAILABLE))
        JarOutputStream(source.outputStream()).use {
            it.putNextEntry(JarEntry(entry))
            it.write(bytes)
            it.closeEntry()
        }
        task.excludePackages.set(setOf("okhttp3"))
        assertArrayEquals(bytes, run(TransportCapability.AVAILABLE))
        assertFalse(bytes.contentEquals(transformed))
    }
}
