package io.jankhunter.gradle

import java.io.File
import java.util.jar.JarEntry
import java.util.jar.JarFile
import java.util.jar.JarOutputStream
import okhttp3.OkHttpClient
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter

/** JVM and ART use the same production visitor against the actual pinned dependency. */
object OkHttpTransportFixture {
    @JvmStatic
    fun main(args: Array<String>) {
        val original = File(OkHttpClient::class.java.protectionDomain.codeSource.location.toURI())
        val output = File(args.single())
        output.parentFile.mkdirs()
        JarFile(original).use { jar ->
            JarOutputStream(output.outputStream()).use { target ->
                for (entry in jar.entries().asSequence().sortedBy { it.name }) {
                    val bytes = jar.getInputStream(entry).use { it.readBytes() }
                    val className = entry.name.removeSuffix(".class").replace('/', '.')
                    val transformed = if (entry.name.endsWith(".class") && OkHttpTransportClassVisitor.matches(className)) {
                        val reader = ClassReader(bytes)
                        val writer = ClassWriter(reader, ClassWriter.COMPUTE_MAXS)
                        reader.accept(OkHttpTransportClassVisitor(writer), 0)
                        writer.toByteArray()
                    } else bytes
                    target.putNextEntry(JarEntry(entry.name).apply { time = 0 })
                    target.write(transformed)
                    target.closeEntry()
                }
            }
        }
    }
}
