package io.jankhunter.gradle

import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import java.io.Serializable
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

internal data class HookAbiClass(
    val access: Int,
    val parent: String?,
    val interfaces: List<String>,
    val methods: Map<String, Int>,
    val fields: Map<String, Int>,
) : Serializable

/** Serializable AGP input containing SDK headers only; no loader, Project, parser or live service. */
class HookAbiIndex internal constructor(
    private val classes: Map<String, HookAbiClass>,
    private val deferred: Boolean,
    private val requiredOwners: Set<String> = classes.keys,
) : Serializable {
    internal fun validateMethod(opcode: Int, owner: String, name: String, descriptor: String, isInterface: Boolean? = null) {
        if (deferred || owner !in OWNERS) return
        if (requiredOwners.isNotEmpty() && owner !in requiredOwners) return
        val header = classes[owner] ?: error("Missing Jank Hunter SDK hook owner $owner")
        check(header.access and Opcodes.ACC_PUBLIC != 0) { "Inaccessible Jank Hunter SDK hook owner $owner" }
        if (isInterface != null) check((header.access and Opcodes.ACC_INTERFACE != 0) == isInterface) {
            "Incompatible Jank Hunter SDK class/interface ABI: $owner"
        }
        val key = name + descriptor
        val access = member(owner, key, field = false, visited = HashSet())
        if (access == null && opcode != Opcodes.INVOKESTATIC && name != "<init>") {
            if (key in OBJECT_METHODS) return
            // Methods inherited from AndroidX/framework parents are not JH hook declarations.
            if (header.parent != null && header.parent != "java/lang/Object" && header.parent !in classes) return
        }
        requireMember(access, opcode == Opcodes.INVOKESTATIC, "$owner.$key")
    }

    private fun validateField(opcode: Int, owner: String, name: String, descriptor: String) {
        if (deferred || owner !in OWNERS) return
        if (requiredOwners.isNotEmpty() && owner !in requiredOwners) return
        requireMember(member(owner, name + descriptor, field = true, visited = HashSet()),
            opcode == Opcodes.GETSTATIC || opcode == Opcodes.PUTSTATIC, "$owner.$name:$descriptor")
    }

    private fun requireMember(access: Int?, static: Boolean, symbol: String) {
        check(access != null && access and Opcodes.ACC_PUBLIC != 0 && (access and Opcodes.ACC_STATIC != 0) == static) {
            "Incompatible Jank Hunter SDK hook ABI: $symbol; use a runtime matching the enabled instrumentation"
        }
    }

    private fun member(owner: String, key: String, field: Boolean, visited: MutableSet<String>): Int? {
        if (!visited.add(owner)) return null
        val header = classes[owner] ?: return null
        (if (field) header.fields else header.methods)[key]?.let { return it }
        return (listOfNotNull(header.parent) + header.interfaces).firstNotNullOfOrNull { member(it, key, field, visited) }
    }

    internal fun visitor(next: ClassVisitor): ClassVisitor {
        if (deferred) return next
        return object : ClassVisitor(Opcodes.ASM9, next) {
            override fun visit(version: Int, access: Int, name: String, signature: String?, superName: String?, interfaces: Array<out String>?) {
                if (superName in OWNERS) {
                    val parent = classes[superName] ?: error("Missing Jank Hunter SDK superclass $superName")
                    check(parent.access and (Opcodes.ACC_FINAL or Opcodes.ACC_INTERFACE) == 0) {
                        "Incompatible Jank Hunter SDK superclass: $superName"
                    }
                }
                super.visit(version, access, name, signature, superName, interfaces)
            }
            override fun visitMethod(access: Int, name: String, descriptor: String, signature: String?, exceptions: Array<out String>?): MethodVisitor? {
                val downstream = super.visitMethod(access, name, descriptor, signature, exceptions) ?: return null
                return object : MethodVisitor(Opcodes.ASM9, downstream) {
                    override fun visitMethodInsn(opcode: Int, owner: String, name: String, descriptor: String, isInterface: Boolean) {
                        validateMethod(opcode, owner, name, descriptor, isInterface)
                        super.visitMethodInsn(opcode, owner, name, descriptor, isInterface)
                    }
                    override fun visitFieldInsn(opcode: Int, owner: String, name: String, descriptor: String) {
                        validateField(opcode, owner, name, descriptor)
                        super.visitFieldInsn(opcode, owner, name, descriptor)
                    }
                }
            }
        }
    }

    internal fun write(file: File) {
        file.parentFile.mkdirs()
        DataOutputStream(file.outputStream().buffered()).use { out ->
            out.writeInt(2); out.writeBoolean(deferred)
            out.writeInt(requiredOwners.size)
            requiredOwners.sorted().forEach(out::writeUTF)
            out.writeInt(classes.size)
            classes.toSortedMap().forEach { (owner, header) ->
                out.writeUTF(owner); out.writeInt(header.access); out.writeUTF(header.parent.orEmpty())
                out.writeInt(header.interfaces.size); header.interfaces.sorted().forEach(out::writeUTF)
                listOf(header.methods, header.fields).forEach { members ->
                    out.writeInt(members.size)
                    members.toSortedMap().forEach { (symbol, flags) -> out.writeUTF(symbol); out.writeInt(flags) }
                }
            }
        }
    }

    companion object {
        internal val OWNERS = setOf(
            "io/jankhunter/runtime/JankHunter", "io/jankhunter/runtime/JankHunterHooks",
            "io/jankhunter/runtime/JankHunterIOHooks", "io/jankhunter/runtime/JankHunterAndroidHooks",
            "io/jankhunter/runtime/JankHunterResources", "io/jankhunter/runtime/JankHunterBindingAccessor",
            "io/jankhunter/runtime/JankHunterLifecycleAccessorV1", "io/jankhunter/runtime/JankHunterLifecycleTargetSinkV1",
            "io/jankhunter/runtime/JankHunterTrackedExecutor", "io/jankhunter/runtime/JankHunterTrackedHandler",
            "io/jankhunter/okhttp3/JankHunterHttpTransportV1", "io/jankhunter/okhttp3/JankHunterOkHttp3",
            "io/jankhunter/workmanager/JankHunterWorker", "io/jankhunter/workmanager/JankHunterCoroutineWorker",
        )
        private val OBJECT_METHODS = Any::class.java.methods.map { it.name + Type.getMethodDescriptor(it) }.toSet()

        internal fun requireRuntimeSurface(compile: Map<String, HookAbiClass>, runtime: Map<String, HookAbiClass>) {
            compile.forEach { (owner, expected) ->
                val actual = runtime[owner] ?: error("Jank Hunter SDK missing from runtime classpath: $owner")
                check(expected.access and Opcodes.ACC_INTERFACE == actual.access and Opcodes.ACC_INTERFACE) {
                    "Jank Hunter SDK compile/runtime class kind mismatch: $owner"
                }
                check(expected.parent == actual.parent && actual.interfaces.containsAll(expected.interfaces)) {
                    "Jank Hunter SDK compile/runtime hierarchy mismatch: $owner"
                }
                check(actual.access and Opcodes.ACC_FINAL == 0 || expected.access and Opcodes.ACC_FINAL != 0) {
                    "Jank Hunter SDK runtime became final: $owner"
                }
                listOf(expected.methods to actual.methods, expected.fields to actual.fields).forEach { (before, after) ->
                    before.filterValues { it and Opcodes.ACC_PUBLIC != 0 }.forEach { (symbol, flags) ->
                        val runtimeFlags = after[symbol] ?: error("Jank Hunter SDK compile/runtime ABI mismatch: $owner.$symbol")
                        check(runtimeFlags and (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC) == flags and (Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC)) {
                            "Jank Hunter SDK compile/runtime ABI flags mismatch: $owner.$symbol"
                        }
                    }
                }
            }
        }

        internal fun read(file: File): HookAbiIndex = DataInputStream(file.inputStream().buffered()).use { input ->
            when (val protocol = input.readInt()) {
                1 -> readProtocol1(input, protocol)
                2 -> readProtocol2(input)
                else -> error("Unsupported Jank Hunter hook ABI report protocol: $protocol")
            }
        }

        private fun readProtocol1(input: DataInputStream, protocol: Int): HookAbiIndex {
            check(protocol == 1)
            val deferred = input.readBoolean()
            val count = input.readInt(); check(count in 0..OWNERS.size)
            val classes = readClasses(input, count)
            check(input.read() == -1) { "Unexpected trailing hook ABI report data" }
            return HookAbiIndex(classes, deferred, classes.keys)
        }

        private fun readProtocol2(input: DataInputStream): HookAbiIndex {
            val deferred = input.readBoolean()
            val requiredCount = input.readInt(); check(requiredCount in 0..OWNERS.size)
            val required = LinkedHashSet<String>()
            repeat(requiredCount) {
                val owner = input.readUTF(); check(owner in OWNERS && owner !in required); required += owner
            }
            val count = input.readInt(); check(count in 0..OWNERS.size)
            val classes = readClasses(input, count)
            check(input.read() == -1) { "Unexpected trailing hook ABI report data" }
            return HookAbiIndex(classes, deferred, required)
        }

        private fun readClasses(input: DataInputStream, count: Int): LinkedHashMap<String, HookAbiClass> {
            val classes = LinkedHashMap<String, HookAbiClass>()
            repeat(count) {
                val owner = input.readUTF(); check(owner in OWNERS && owner !in classes)
                val access = input.readInt(); val parent = input.readUTF().ifEmpty { null }
                val interfaceCount = input.readInt(); check(interfaceCount in 0..128)
                val interfaces = List(interfaceCount) { input.readUTF() }
                val members = (1..2).map {
                    val size = input.readInt(); check(size in 0..4096)
                    val values = LinkedHashMap<String, Int>()
                    repeat(size) { values[input.readUTF()] = input.readInt() }
                    values
                }
                classes[owner] = HookAbiClass(access, parent, interfaces, members[0], members[1])
            }
            return classes
        }
    }
}
