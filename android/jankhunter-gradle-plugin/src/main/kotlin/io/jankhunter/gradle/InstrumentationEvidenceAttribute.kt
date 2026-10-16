package io.jankhunter.gradle

import java.io.File
import java.util.zip.ZipFile
import org.objectweb.asm.Attribute
import org.objectweb.asm.ByteVector
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.Opcodes

/** Build-only evidence travels with AGP's cached class output; D8 does not emit unknown attributes. */
internal class InstrumentationEvidenceAttribute private constructor(
    val kind: String,
    private val payload: ByteArray,
) : Attribute("JankHunterBuildEvidenceV1_$kind") {
    constructor(kind: String, json: String = "") : this(kind, json.toByteArray(Charsets.UTF_8))
    val json: String get() = payload.toString(Charsets.UTF_8)
    private var encoded: ByteVector? = null

    override fun write(writer: ClassWriter, code: ByteArray?, length: Int, maxStack: Int, maxLocals: Int): ByteVector =
        encoded ?: ByteVector(payload.size).putByteArray(payload, 0, payload.size).also { encoded = it }

    override fun read(reader: ClassReader, offset: Int, length: Int, buffer: CharArray?, codeOffset: Int, labels: Array<out Label>?): Attribute {
        val bytes = ByteArray(length) { reader.readByte(offset + it).toByte() }
        return InstrumentationEvidenceAttribute(type.removePrefix("JankHunterBuildEvidenceV1_"), bytes)
    }

    companion object {
        fun forEachRecord(jars: List<File>, directories: List<File>, consume: (String, String) -> Unit) {
            val prototypes = arrayOf("main", "causal", "lifecycle", "lifecycle_metadata", "class_graph", "lambda_capture", "android_component", "di")
                .map { InstrumentationEvidenceAttribute(it) }.toTypedArray<Attribute>()
            fun read(bytes: ByteArray) {
                ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
                    override fun visitAttribute(attribute: Attribute) {
                        if (attribute is InstrumentationEvidenceAttribute) consume(attribute.kind, attribute.json)
                    }
                }, prototypes, ClassReader.SKIP_CODE or ClassReader.SKIP_DEBUG or ClassReader.SKIP_FRAMES)
            }
            jars.sortedBy(File::getPath).forEach { file ->
                ZipFile(file).use { zip ->
                    zip.entries().asSequence().filter { it.name.endsWith(".class") && !it.name.startsWith("META-INF/") }.sortedBy { it.name }
                        .forEach { entry -> zip.getInputStream(entry).use { read(it.readBytes()) } }
                }
            }
            directories.sortedBy(File::getPath).forEach { root ->
                root.walkTopDown().filter { it.isFile && it.extension == "class" }.sortedBy { it.relativeTo(root).invariantSeparatorsPath }
                    .forEach { read(it.readBytes()) }
            }
        }
    }
}
