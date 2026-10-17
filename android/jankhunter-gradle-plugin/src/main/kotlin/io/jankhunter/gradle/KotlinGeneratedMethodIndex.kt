package io.jankhunter.gradle

import kotlin.Metadata as KotlinMetadata
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.Opcodes

internal class KotlinGeneratedMethodIndex private constructor(
    private val generatedMethods: Map<String, String>,
    private val declaredMethods: Set<String>,
    private val metadataKnown: Boolean,
    private val syntheticClass: Boolean,
    val delegatedProperties: Map<String, String?> = emptyMap(),
    val analysisStatus: KotlinMetadataAnalysisStatus = KotlinMetadataAnalysisStatus.READABLE,
    val readerId: String? = null,
    val metadataVersion: String = "",
) {
    fun origin(methodName: String, descriptor: String): KotlinMethodOrigin {
        if (!metadataKnown) return KotlinMethodOrigin.UNKNOWN
        if (syntheticClass) return KotlinMethodOrigin.generated(KOTLIN_SYNTHETIC_CLASS)
        val key = methodKey(methodName, descriptor)
        generatedMethods[key]?.let { return KotlinMethodOrigin.generated(it) }
        if (key in declaredMethods) return KotlinMethodOrigin.DECLARED
        // Metadata describes declarations, not every JVM method emitted for local functions,
        // suspend transformations and anonymous implementations. Absence is not proof that a
        // method is generated: let the bytecode classifier make the conservative decision.
        return KotlinMethodOrigin.UNKNOWN
    }

    fun reason(methodName: String, descriptor: String): String? {
        return origin(methodName, descriptor).generatedReason
    }

    companion object {
        val EMPTY = KotlinGeneratedMethodIndex(
            generatedMethods = emptyMap(),
            declaredMethods = emptySet(),
            metadataKnown = false,
            syntheticClass = false,
            analysisStatus = KotlinMetadataAnalysisStatus.ABSENT,
        )
        const val METADATA_DESCRIPTOR = "Lkotlin/Metadata;"

        fun from(
            metadata: KotlinMetadata,
            mode: JankHunterMetadataCompatibilityMode = JankHunterMetadataCompatibilityMode.COMPATIBILITY,
            className: String = "unknown",
            pathway: MetadataDecodePathway = MetadataDecodePathway.MAIN,
        ): KotlinGeneratedMethodIndex {
            val version = metadata.metadataVersion.take(4).joinToString(".")
            MetadataReaderMetrics.recordDecodeAttempt(pathway)
            val result = IsolatedMetadataReader.read(metadata)
            if (mode == JankHunterMetadataCompatibilityMode.STRICT && result.status != KotlinMetadataAnalysisStatus.READABLE) {
                mode.validate(result.status, className, version)
            }
            val values = result.values
            if (values == null) {
                val index = fallback(result.status, version, result.readerId)
                return index
            }
            return KotlinGeneratedMethodIndex(values.generated, values.declared, values.known,
                values.synthetic, values.delegates, values.status, result.readerId, version)
        }

        internal fun unreadable(className: String, mode: JankHunterMetadataCompatibilityMode): KotlinGeneratedMethodIndex {
            val status = KotlinMetadataAnalysisStatus.UNREADABLE
            if (mode == JankHunterMetadataCompatibilityMode.STRICT) {
                mode.validate(status, className, "")
            }
            return fallback(status)
        }

        private fun fallback(status: KotlinMetadataAnalysisStatus, version: String = "", readerId: String? = null) = KotlinGeneratedMethodIndex(
            emptyMap(), emptySet(), metadataKnown = false, syntheticClass = false, analysisStatus = status,
            readerId = readerId, metadataVersion = version,
        )

        fun collectingVisitor(
            delegate: AnnotationVisitor?,
            mode: JankHunterMetadataCompatibilityMode = JankHunterMetadataCompatibilityMode.COMPATIBILITY,
            className: String = "unknown",
            pathway: MetadataDecodePathway = MetadataDecodePathway.MAIN,
            completed: (KotlinGeneratedMethodIndex) -> Unit,
        ): AnnotationVisitor = KotlinMetadataAnnotationVisitor(delegate, mode, className, pathway, completed)

        private fun methodKey(name: String, descriptor: String): String = name + descriptor

        private const val KOTLIN_SYNTHETIC_CLASS = "kotlin_synthetic_class"
    }
}

internal enum class KotlinMetadataAnalysisStatus {
    ABSENT, READABLE, UNSUPPORTED_VERSION, UNREADABLE, UNSUPPORTED_KIND, UNSUPPORTED_FLAGS, READER_UNAVAILABLE,
}

internal data class KotlinMethodOrigin(
    val metadataKnown: Boolean,
    val generatedReason: String?,
) {
    companion object {
        val UNKNOWN = KotlinMethodOrigin(metadataKnown = false, generatedReason = null)
        val DECLARED = KotlinMethodOrigin(metadataKnown = true, generatedReason = null)

        fun generated(reason: String) = KotlinMethodOrigin(metadataKnown = true, generatedReason = reason)
    }
}

private class KotlinMetadataAnnotationVisitor(
    delegate: AnnotationVisitor?,
    private val mode: JankHunterMetadataCompatibilityMode,
    private val className: String,
    private val pathway: MetadataDecodePathway,
    private val completed: (KotlinGeneratedMethodIndex) -> Unit,
) : AnnotationVisitor(Opcodes.ASM9, delegate) {
    private var kind: Int? = null
    private var metadataVersion: IntArray? = null
    private var data1: Array<String>? = null
    private var data2: Array<String>? = null
    private var extraString: String? = null
    private var packageName: String? = null
    private var extraInt: Int? = null

    override fun visit(name: String?, value: Any?) {
        when (name) {
            "k" -> kind = value as? Int
            "mv" -> metadataVersion = value as? IntArray
            "d1" -> data1 = value.asStringArray()
            "d2" -> data2 = value.asStringArray()
            "xs" -> extraString = value as? String
            "pn" -> packageName = value as? String
            "xi" -> extraInt = value as? Int
        }
        super.visit(name, value)
    }

    override fun visitArray(name: String?): AnnotationVisitor {
        val values = ArrayList<Any>()
        return object : AnnotationVisitor(Opcodes.ASM9, super.visitArray(name)) {
            override fun visit(elementName: String?, value: Any?) {
                if (value != null) values += value
                super.visit(elementName, value)
            }

            override fun visitEnd() {
                when (name) {
                    "mv" -> metadataVersion = values.filterIsInstance<Int>().toIntArray()
                    "d1" -> data1 = values.filterIsInstance<String>().toTypedArray()
                    "d2" -> data2 = values.filterIsInstance<String>().toTypedArray()
                }
                super.visitEnd()
            }
        }
    }

    override fun visitEnd() {
        super.visitEnd()
        val version = metadataVersion
        val kindValue = kind
        if (kindValue == null || version == null || version.size !in 2..3 || version.any { it < 0 }) {
            completed(KotlinGeneratedMethodIndex.unreadable(className, mode))
            return
        }
        val metadata = KotlinMetadata(
            kind = kindValue,
            metadataVersion = version,
            data1 = data1 ?: emptyArray(),
            data2 = data2 ?: emptyArray(),
            extraString = extraString ?: "",
            packageName = packageName ?: "",
            extraInt = extraInt ?: 0,
        )
        completed(KotlinGeneratedMethodIndex.from(metadata, mode, className, pathway))
    }

    private fun Any?.asStringArray(): Array<String>? {
        return when (this) {
            is Array<*> -> filterIsInstance<String>().toTypedArray()
            else -> null
        }
    }
}
