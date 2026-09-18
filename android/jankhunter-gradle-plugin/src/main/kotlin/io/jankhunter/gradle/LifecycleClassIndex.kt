package io.jankhunter.gradle

import java.io.File
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.FieldVisitor
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

/** Build-time hierarchy only; no class loading, initialization, or runtime name inference. */
internal class LifecycleClassIndex(file: File? = null) : AutoCloseable {
    private val storage = LifecycleMetadataStore(file)

    fun add(bytes: ByteArray, programClass: Boolean): String {
        val reader = ClassReader(bytes)
        val existing = header(reader.className)
        if (existing != null && !programClass) return reader.className
        require(existing?.programClass != true || !programClass) { "Duplicate program class: ${reader.className}" }
        val methods = HashMap<String, Int>()
        val annotations = HashSet<String>()
        val accessorMethods = HashMap<String, Int>()
        var hasReferenceFields = false
        reader.accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visitField(access: Int, name: String, descriptor: String, signature: String?, value: Any?): FieldVisitor? {
                if (access and Opcodes.ACC_STATIC == 0 && descriptor.startsWith('L')) hasReferenceFields = true
                return null
            }
            override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
                if (descriptor == "Lio/jankhunter/annotations/JankHunterIgnore;" ||
                    descriptor == LifecycleScopedTransformer.ACCESSOR_MARKER) annotations.add(descriptor)
                return null
            }
            override fun visitMethod(
                access: Int,
                name: String,
                descriptor: String,
                signature: String?,
                exceptions: Array<out String>?,
            ): MethodVisitor? {
                if (descriptor == "()V" && name in CALLBACK_NAMES) methods[name] = access
                if (name == LifecycleAccessorEmitter.KIND_METHOD || name == LifecycleAccessorEmitter.VISIT_METHOD ||
                    reader.className == LifecycleAccessorEmitter.SINK && name == "accept" ||
                    reader.className == "io/jankhunter/runtime/JankHunterHooks" && name == "watchLifecycleObject") {
                    accessorMethods[name + descriptor] = access
                }
                return null
            }
        }, ClassReader.SKIP_CODE or ClassReader.SKIP_DEBUG or ClassReader.SKIP_FRAMES)
        val header = LifecycleClassHeader(
            reader.className, reader.superName, reader.interfaces.toList(), reader.access,
            methods.toMap(), programClass, annotations, accessorMethods, hasReferenceFields,
        )
        storage.put(header)
        return header.name
    }

    fun header(name: String): LifecycleClassHeader? = storage.header(name)
    fun programClasses(): Sequence<LifecycleClassHeader> = storage.headers().filter { it.programClass }
    fun <T> newWorkMap(): MutableMap<String, T> = storage.newMap()
    fun maintainBudget() = storage.maintainBudget()
    override fun close() = storage.close()

    fun hasExplicitBindingAccessor(name: String): Boolean {
        val pending = java.util.ArrayDeque<String>().apply { add(name) }
        val seen = HashSet<String>()
        while (pending.isNotEmpty()) {
            val current = pending.removeFirst()
            if (current == "io/jankhunter/runtime/JankHunterBindingAccessor") return true
            if (!seen.add(current)) continue
            val header = header(current) ?: continue
            header.superName?.let(pending::addLast)
            header.interfaces.forEach(pending::addLast)
        }
        return false
    }

    fun kind(name: String): LifecycleTargetKind {
        for (owner in superclasses(name)) {
            LifecycleTargetKind.fromRoot(owner)?.let { return it }
        }
        return LifecycleTargetKind.NONE
    }

    fun superclasses(name: String): List<String> {
        val result = ArrayList<String>()
        val seen = HashSet<String>()
        var current: String? = name
        while (current != null) {
            check(seen.add(current)) { "Cyclic lifecycle hierarchy at $current" }
            result.add(current)
            current = header(current)?.superName
        }
        return result
    }

    fun callback(name: String, method: String): LifecycleCallbackResolution {
        for (owner in superclasses(name)) {
            val header = header(owner) ?: return LifecycleCallbackResolution.MissingClass(owner)
            val access = header.callbackAccess[method] ?: continue
            return LifecycleCallbackResolution.Declaration(owner, access, header.programClass)
        }
        return LifecycleCallbackResolution.Absent
    }

    companion object {
        private val CALLBACK_NAMES = setOf("onDestroy", "onDestroyView", "onCleared")
    }
}

internal data class LifecycleClassHeader(
    val name: String,
    val superName: String?,
    val interfaces: List<String>,
    val access: Int,
    val callbackAccess: Map<String, Int>,
    val programClass: Boolean,
    val annotations: Set<String>,
    val accessorMethods: Map<String, Int>,
    val hasReferenceFields: Boolean,
)

internal enum class LifecycleTargetKind(val code: Int, val callbacks: List<String>) {
    NONE(0, emptyList()),
    ACTIVITY(1, listOf("onDestroy")),
    FRAGMENT(2, listOf("onDestroyView", "onDestroy")),
    VIEW_MODEL(3, listOf("onCleared")),
    SERVICE(4, listOf("onDestroy")),
    ;

    companion object {
        fun fromRoot(name: String): LifecycleTargetKind? = when (name) {
            "android/app/Activity" -> ACTIVITY
            "android/app/Fragment", "androidx/fragment/app/Fragment", "android/support/v4/app/Fragment" -> FRAGMENT
            "androidx/lifecycle/ViewModel", "android/arch/lifecycle/ViewModel" -> VIEW_MODEL
            "android/app/Service" -> SERVICE
            else -> null
        }
    }
}

internal sealed interface LifecycleCallbackResolution {
    data class Declaration(val owner: String, val access: Int, val programClass: Boolean) : LifecycleCallbackResolution {
        fun canOverrideFrom(subclass: String): Boolean {
            if (access and (Opcodes.ACC_FINAL or Opcodes.ACC_STATIC or Opcodes.ACC_PRIVATE) != 0) return false
            return access and (Opcodes.ACC_PUBLIC or Opcodes.ACC_PROTECTED) != 0 ||
                owner.substringBeforeLast('/', "") == subclass.substringBeforeLast('/', "")
        }
        val hasBody: Boolean get() = access and (Opcodes.ACC_ABSTRACT or Opcodes.ACC_NATIVE) == 0
    }
    data class MissingClass(val name: String) : LifecycleCallbackResolution
    data object Absent : LifecycleCallbackResolution
}
