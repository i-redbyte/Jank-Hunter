package io.jankhunter.gradle

import kotlin.Metadata as KotlinMetadata
import kotlin.metadata.ClassKind
import kotlin.metadata.KmClass
import kotlin.metadata.KmDeclarationContainer
import kotlin.metadata.MemberKind
import kotlin.metadata.isNotDefault
import kotlin.metadata.kind
import kotlin.metadata.jvm.KotlinClassMetadata
import kotlin.metadata.jvm.getterSignature
import kotlin.metadata.jvm.setterSignature
import kotlin.metadata.jvm.signature
import kotlin.metadata.jvm.syntheticMethodForAnnotations
import kotlin.metadata.jvm.syntheticMethodForDelegate
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.Opcodes

internal class KotlinGeneratedMethodIndex private constructor(
    private val generatedMethods: Map<String, String>,
    private val declaredMethods: Set<String>,
    private val metadataKnown: Boolean,
    private val syntheticClass: Boolean,
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
        )
        const val METADATA_DESCRIPTOR = "Lkotlin/Metadata;"

        fun from(metadata: KotlinMetadata): KotlinGeneratedMethodIndex {
            return when (val parsed = runCatching { KotlinClassMetadata.readLenient(metadata) }.getOrNull()) {
                is KotlinClassMetadata.Class -> fromClass(parsed.kmClass)
                is KotlinClassMetadata.FileFacade -> fromContainer(parsed.kmPackage)
                is KotlinClassMetadata.MultiFileClassPart -> fromContainer(parsed.kmPackage)
                is KotlinClassMetadata.SyntheticClass -> KotlinGeneratedMethodIndex(
                    emptyMap(),
                    emptySet(),
                    metadataKnown = true,
                    syntheticClass = true,
                )
                is KotlinClassMetadata.MultiFileClassFacade -> KotlinGeneratedMethodIndex(
                    emptyMap(),
                    emptySet(),
                    metadataKnown = true,
                    syntheticClass = false,
                )
                else -> EMPTY
            }
        }

        fun collectingVisitor(
            delegate: AnnotationVisitor?,
            completed: (KotlinGeneratedMethodIndex) -> Unit,
        ): AnnotationVisitor = KotlinMetadataAnnotationVisitor(delegate, completed)

        private fun fromContainer(container: KmDeclarationContainer): KotlinGeneratedMethodIndex {
            val methods = HashMap<String, String>()
            val declared = HashSet<String>()
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
            return KotlinGeneratedMethodIndex(
                generatedMethods = methods,
                declaredMethods = declared,
                metadataKnown = true,
                syntheticClass = false,
            )
        }

        private fun fromClass(kmClass: KmClass): KotlinGeneratedMethodIndex {
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
            return KotlinGeneratedMethodIndex(
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
        val metadata = kotlin.metadata.jvm.Metadata(
            kind = kind,
            metadataVersion = metadataVersion,
            data1 = data1,
            data2 = data2,
            extraString = extraString,
            packageName = packageName,
            extraInt = extraInt,
        )
        completed(KotlinGeneratedMethodIndex.from(metadata))
    }

    private fun Any?.asStringArray(): Array<String>? {
        return when (this) {
            is Array<*> -> filterIsInstance<String>().toTypedArray()
            else -> null
        }
    }
}
