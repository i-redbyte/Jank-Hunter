package io.jankhunter.reader

import kotlin.Metadata as KotlinMetadata
import kotlin.metadata.ClassKind
import kotlin.metadata.KmClass
import kotlin.metadata.KmDeclarationContainer
import kotlin.metadata.MemberKind
import kotlin.metadata.isDelegated
import kotlin.metadata.isNotDefault
import kotlin.metadata.kind
import kotlin.metadata.jvm.KotlinClassMetadata
import kotlin.metadata.jvm.JvmMetadataVersion
import kotlin.metadata.jvm.fieldSignature
import kotlin.metadata.jvm.getterSignature
import kotlin.metadata.jvm.setterSignature
import kotlin.metadata.jvm.signature
import kotlin.metadata.jvm.syntheticMethodForAnnotations
import kotlin.metadata.jvm.syntheticMethodForDelegate

internal class IsolatedMetadataIndex private constructor(
    private val generatedMethods: Map<String, String>,
    private val declaredMethods: Set<String>,
    private val metadataKnown: Boolean,
    private val syntheticClass: Boolean,
    val delegatedProperties: Map<String, String?> = emptyMap(),
    val analysisStatus: KotlinMetadataAnalysisStatus = KotlinMetadataAnalysisStatus.READABLE,
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

        @JvmStatic
        fun read(kind: Int, version: IntArray, data1: Array<String>, data2: Array<String>,
                 extraString: String, packageName: String, extraInt: Int): java.util.HashMap<String, Any> {
            val index = from(kotlin.metadata.jvm.Metadata(kind = kind, metadataVersion = version,
                data1 = data1, data2 = data2, extraString = extraString, packageName = packageName, extraInt = extraInt))
            return java.util.HashMap<String, Any>().apply {
                put("protocol", 1)
                put("generated", java.util.HashMap(index.generatedMethods))
                put("declared", java.util.HashSet(index.declaredMethods))
                put("delegates", java.util.HashMap(index.delegatedProperties))
                put("known", index.metadataKnown)
                put("synthetic", index.syntheticClass)
                put("status", index.analysisStatus.name)
            }
        }

        val EMPTY = IsolatedMetadataIndex(
            generatedMethods = emptyMap(),
            declaredMethods = emptySet(),
            metadataKnown = false,
            syntheticClass = false,
            analysisStatus = KotlinMetadataAnalysisStatus.ABSENT,
        )
        const val METADATA_DESCRIPTOR = "Lkotlin/Metadata;"

        fun from(metadata: KotlinMetadata): IsolatedMetadataIndex {
            val version = metadata.metadataVersion
            val supported = JvmMetadataVersion.LATEST_STABLE_SUPPORTED
            if (version.size < 2) return fallback(KotlinMetadataAnalysisStatus.UNREADABLE)
            if (version[0] > supported.major || (version[0] == supported.major && version[1] > supported.minor)) {
                // Lenient parsing can succeed while omitting unknown declarations. That is not
                // proof that excluding an application method preserves diagnostic coverage.
                return fallback(KotlinMetadataAnalysisStatus.UNSUPPORTED_VERSION)
            }
            return try {
                when (val parsed = KotlinClassMetadata.readStrict(metadata)) {
                    is KotlinClassMetadata.Class -> fromClass(parsed.kmClass)
                    is KotlinClassMetadata.FileFacade -> fromContainer(parsed.kmPackage)
                    is KotlinClassMetadata.MultiFileClassPart -> fromContainer(parsed.kmPackage)
                    is KotlinClassMetadata.SyntheticClass -> IsolatedMetadataIndex(
                        emptyMap(),
                        emptySet(),
                        metadataKnown = true,
                        syntheticClass = true,
                    )
                    is KotlinClassMetadata.MultiFileClassFacade -> IsolatedMetadataIndex(
                        emptyMap(),
                        emptySet(),
                        metadataKnown = true,
                        syntheticClass = false,
                    )
                    else -> fallback(KotlinMetadataAnalysisStatus.UNSUPPORTED_KIND)
                }
            } catch (failure: Exception) {
                // Include index construction in the boundary, not just protobuf parsing.
                fallback(KotlinMetadataAnalysisStatus.UNREADABLE)
            }
        }

        private fun fallback(status: KotlinMetadataAnalysisStatus) = IsolatedMetadataIndex(
            emptyMap(), emptySet(), metadataKnown = false, syntheticClass = false, analysisStatus = status,
        )

        private fun fromContainer(container: KmDeclarationContainer): IsolatedMetadataIndex {
            val methods = HashMap<String, String>()
            val declared = HashSet<String>()
            val delegates = LinkedHashMap<String, String?>()
            container.functions.forEach { function ->
                function.signature?.let { signature ->
                    val key = methodKey(signature.name, signature.descriptor)
                    if (function.kind == MemberKind.DECLARATION) {
                        declared += key
                    } else {
                        methods[methodKey(signature.name, signature.descriptor)] = KOTLIN_GENERATED_FUNCTION
                    }
                }
            }
            container.properties.forEach { property ->
                if (property.isDelegated) delegates[property.name] = property.fieldSignature?.descriptor
                val generatedProperty = property.kind != MemberKind.DECLARATION
                if (generatedProperty || !property.getter.isNotDefault) {
                    property.getterSignature?.let { signature ->
                        methods[methodKey(signature.name, signature.descriptor)] = KOTLIN_DEFAULT_ACCESSOR
                    }
                } else {
                    property.getterSignature?.let { signature ->
                        declared += methodKey(signature.name, signature.descriptor)
                    }
                }
                val setter = property.setter
                if (setter != null && (generatedProperty || !setter.isNotDefault)) {
                    property.setterSignature?.let { signature ->
                        methods[methodKey(signature.name, signature.descriptor)] = KOTLIN_DEFAULT_ACCESSOR
                    }
                } else if (setter != null) {
                    property.setterSignature?.let { signature ->
                        declared += methodKey(signature.name, signature.descriptor)
                    }
                }
                property.syntheticMethodForAnnotations?.let { signature ->
                    methods[methodKey(signature.name, signature.descriptor)] = KOTLIN_SYNTHETIC_PROPERTY_METHOD
                }
                property.syntheticMethodForDelegate?.let { signature ->
                    methods[methodKey(signature.name, signature.descriptor)] = KOTLIN_SYNTHETIC_PROPERTY_METHOD
                }
            }
            return IsolatedMetadataIndex(
                delegatedProperties = delegates,
                generatedMethods = methods,
                declaredMethods = declared,
                metadataKnown = true,
                syntheticClass = false,
            )
        }

        private fun fromClass(kmClass: KmClass): IsolatedMetadataIndex {
            val base = fromContainer(kmClass)
            if (kmClass.kind != ClassKind.CLASS && kmClass.kind != ClassKind.ENUM_CLASS) return base
            val generatedMethods = if (kmClass.kind == ClassKind.ENUM_CLASS) {
                base.generatedMethods + (KOTLIN_ENUM_ENTRIES_METHOD_KEY to KOTLIN_ENUM_ENTRIES_ACCESSOR)
            } else {
                base.generatedMethods
            }
            val declaredConstructors = kmClass.constructors.mapNotNullTo(HashSet()) { constructor ->
                constructor.signature?.let { signature -> methodKey(signature.name, signature.descriptor) }
            }
            if (declaredConstructors.isEmpty() && generatedMethods === base.generatedMethods) return base
            return IsolatedMetadataIndex(
                delegatedProperties = base.delegatedProperties,
                generatedMethods = generatedMethods,
                declaredMethods = base.declaredMethods + declaredConstructors,
                metadataKnown = base.metadataKnown,
                syntheticClass = base.syntheticClass,
            )
        }

        private fun methodKey(name: String, descriptor: String): String = name + descriptor

        private const val KOTLIN_GENERATED_FUNCTION = "kotlin_generated_function"
        private const val KOTLIN_DEFAULT_ACCESSOR = "kotlin_default_accessor"
        private const val KOTLIN_SYNTHETIC_PROPERTY_METHOD = "kotlin_synthetic_property_method"
        private const val KOTLIN_SYNTHETIC_CLASS = "kotlin_synthetic_class"
        private const val KOTLIN_ENUM_ENTRIES_METHOD_KEY = "getEntries()Lkotlin/enums/EnumEntries;"
        private const val KOTLIN_ENUM_ENTRIES_ACCESSOR = "kotlin_enum_entries_accessor"
    }
}

internal enum class KotlinMetadataAnalysisStatus {
    ABSENT, READABLE, UNSUPPORTED_VERSION, UNREADABLE, UNSUPPORTED_KIND,
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
