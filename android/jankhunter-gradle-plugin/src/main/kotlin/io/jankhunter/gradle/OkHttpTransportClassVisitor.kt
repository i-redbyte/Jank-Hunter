package io.jankhunter.gradle

import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.AbstractInsnNode
import org.objectweb.asm.tree.AnnotationNode
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.FieldInsnNode
import org.objectweb.asm.tree.FieldNode
import org.objectweb.asm.tree.InsnList
import org.objectweb.asm.tree.InsnNode
import org.objectweb.asm.tree.MethodInsnNode
import org.objectweb.asm.tree.MethodNode
import org.objectweb.asm.tree.VarInsnNode

/** Exact structural hooks for the reviewed OkHttp 3.12 transport, without additional I/O calls. */
internal class OkHttpTransportClassVisitor(private val next: ClassVisitor) : ClassNode(Opcodes.ASM9) {
    override fun visitEnd() {
        super.visitEnd()
        val marked = (invisibleAnnotations.orEmpty() + visibleAnnotations.orEmpty())
            .any { it.desc == InstrumentationMarker.DESCRIPTOR }
        val changed = !marked && when (name) {
            REAL_CONNECTION -> realConnection()
            HTTP2_CODEC -> http2Codec()
            HTTP2_CONNECTION -> http2Connection()
            REAL_CALL -> realCall()
            CACHE_BODY -> cacheBody()
            else -> false
        }
        if (changed) {
            if (invisibleAnnotations == null) invisibleAnnotations = mutableListOf()
            invisibleAnnotations.add(AnnotationNode(InstrumentationMarker.DESCRIPTOR))
        }
        accept(next)
    }

    private fun realConnection(): Boolean {
        if (!canOwnState()) return false
        val socket = methods.singleOrNull { it.name == "connectSocket" } ?: return false
        val tls = methods.singleOrNull { it.name == "connectTls" } ?: return false
        val start = methods.singleOrNull { it.name == "startHttp2" } ?: return false
        val wrapSocket = socket.calls("okio/Okio", "buffer", "(Lokio/Source;)Lokio/BufferedSource;").singleOrNull()
            ?: return false
        val wrapTLS = tls.calls("okio/Okio", "buffer", "(Lokio/Source;)Lokio/BufferedSource;").singleOrNull()
            ?: return false
        val startReader = start.calls(HTTP2_CONNECTION, "start", "()V").singleOrNull() ?: return false
        // Check every anchor before changing this class, so an unknown layout stays untouched.
        for ((method, call) in listOf(socket to wrapSocket, tls to wrapTLS)) {
            method.instructions.insertBefore(call, VarInsnNode(Opcodes.ALOAD, 0))
            method.instructions.set(call, helper("bufferHttpResponseSource", "(Lokio/Source;Ljava/lang/Object;)Lokio/BufferedSource;"))
        }
        start.instructions.insertBefore(startReader, InsnList().apply {
            add(InsnNode(Opcodes.DUP))
            add(VarInsnNode(Opcodes.ALOAD, 0))
            add(helper("bindHttp2Transport", "(Ljava/lang/Object;Ljava/lang/Object;)V"))
        })
        addOwnerState()
        return true
    }

    private fun http2Codec(): Boolean {
        if (fields.none { it.name == "streamAllocation" && it.desc == "L$STREAM_ALLOCATION;" }) return false
        val method = methods.singleOrNull { it.name == "writeRequestHeaders" && it.desc == "(Lokhttp3/Request;)V" }
            ?: return false
        val call = method.calls(HTTP2_CONNECTION, "newStream", "(Ljava/util/List;Z)L$HTTP2_STREAM;").singleOrNull()
            ?: return false
        method.instructions.insertBefore(call, InsnList().apply {
            add(VarInsnNode(Opcodes.ALOAD, 0))
            add(FieldInsnNode(Opcodes.GETFIELD, name, "streamAllocation", "L$STREAM_ALLOCATION;"))
            add(FieldInsnNode(Opcodes.GETFIELD, STREAM_ALLOCATION, "eventListener", "Lokhttp3/EventListener;"))
        })
        method.instructions.set(call, helper("openObservedHttp2Stream",
            "(L$HTTP2_CONNECTION;Ljava/util/List;ZLokhttp3/EventListener;)L$HTTP2_STREAM;"))
        return true
    }

    private fun http2Connection(): Boolean {
        if (!canOwnState()) return false
        val method = methods.singleOrNull { it.name == "newStream" && it.desc == "(ILjava/util/List;Z)L$HTTP2_STREAM;" }
            ?: return false
        val allocatedID = method.instructions.toArray().filterIsInstance<FieldInsnNode>()
            .filter { it.opcode == Opcodes.GETFIELD && it.owner == name && it.name == "nextStreamId" && it.desc == "I" }
            .mapNotNull { nextInstruction(it) as? VarInsnNode }.singleOrNull { it.opcode == Opcodes.ISTORE }
            ?: return false
        val send = method.calls("okhttp3/internal/http2/Http2Writer", "synStream", "(ZIILjava/util/List;)V").singleOrNull()
            ?: return false
        // The real stream ID is allocated under OkHttp's writer lock. Bind before any header write
        // can reach the peer; a response may arrive before newStream itself returns to the caller.
        method.instructions.insertBefore(send, InsnList().apply {
            add(VarInsnNode(Opcodes.ALOAD, 0))
            add(VarInsnNode(Opcodes.ILOAD, allocatedID.`var`))
            add(helper("onHttp2StreamAllocated", "(Ljava/lang/Object;I)V"))
        })
        addOwnerState()
        return true
    }

    private fun realCall(): Boolean {
        if (fields.none { it.name == "eventListener" && it.desc == "Lokhttp3/EventListener;" }) return false
        val method = methods.singleOrNull { it.name == "getResponseWithInterceptorChain" && it.desc == "()Lokhttp3/Response;" }
            ?: return false
        val terminalReturn = method.instructions.toArray().singleOrNull { it.opcode == Opcodes.ARETURN } ?: return false
        method.instructions.insertBefore(terminalReturn, InsnList().apply {
            add(InsnNode(Opcodes.DUP))
            add(VarInsnNode(Opcodes.ALOAD, 0))
            add(FieldInsnNode(Opcodes.GETFIELD, name, "eventListener", "Lokhttp3/EventListener;"))
            add(helper("onHttpResponseReturned", "(Lokhttp3/Response;Lokhttp3/EventListener;)V"))
        })
        return true
    }

    private fun cacheBody(): Boolean {
        if (!canOwnState()) return false
        if (fields.none { it.name == "bodySource" && it.desc == "Lokio/BufferedSource;" }) return false
        val constructor = methods.singleOrNull { it.name == "<init>" } ?: return false
        val wrap = constructor.calls("okio/Okio", "buffer", "(Lokio/Source;)Lokio/BufferedSource;").singleOrNull()
            ?: return false
        constructor.instructions.insertBefore(wrap, VarInsnNode(Opcodes.ALOAD, 0))
        constructor.instructions.set(wrap, helper("bufferCachedResponseSource", "(Lokio/Source;Ljava/lang/Object;)Lokio/BufferedSource;"))
        addOwnerState()
        return true
    }

    private fun canOwnState(): Boolean = OWNER_ABI !in interfaces &&
        fields.none { it.name == STATE_FIELD } && methods.none { it.name == STATE_ACCESS }

    private fun addOwnerState() {
        val owner = name
        interfaces.add(OWNER_ABI)
        fields.add(FieldNode(Opcodes.ACC_PRIVATE or Opcodes.ACC_TRANSIENT or Opcodes.ACC_VOLATILE,
            STATE_FIELD, "Ljava/lang/Object;", null, null))
        methods.add(MethodNode(Opcodes.ACC_PUBLIC or Opcodes.ACC_FINAL, STATE_ACCESS, "()Ljava/lang/Object;", null, null).apply {
            instructions.add(VarInsnNode(Opcodes.ALOAD, 0))
            instructions.add(FieldInsnNode(Opcodes.GETFIELD, owner, STATE_FIELD, "Ljava/lang/Object;"))
            instructions.add(InsnNode(Opcodes.ARETURN))
            maxStack = 1
            maxLocals = 1
        })
        methods.add(MethodNode(Opcodes.ACC_PUBLIC or Opcodes.ACC_FINAL, STATE_ACCESS, "(Ljava/lang/Object;)V", null, null).apply {
            instructions.add(VarInsnNode(Opcodes.ALOAD, 0))
            instructions.add(VarInsnNode(Opcodes.ALOAD, 1))
            instructions.add(FieldInsnNode(Opcodes.PUTFIELD, owner, STATE_FIELD, "Ljava/lang/Object;"))
            instructions.add(InsnNode(Opcodes.RETURN))
            maxStack = 2
            maxLocals = 2
        })
    }

    private fun MethodNode.calls(owner: String, method: String, descriptor: String): List<MethodInsnNode> =
        instructions.toArray().filterIsInstance<MethodInsnNode>()
            .filter { it.owner == owner && it.name == method && it.desc == descriptor }

    private fun nextInstruction(node: AbstractInsnNode): AbstractInsnNode? {
        var next = node.next
        while (next != null && next.opcode < 0) next = next.next
        return next
    }

    private fun helper(method: String, descriptor: String) =
        MethodInsnNode(Opcodes.INVOKESTATIC, HELPER, method, descriptor, false)

    companion object {
        const val OWNER_ABI = "io/jankhunter/okhttp3/JankHunterHttpTransportV1"
        const val HELPER = "io/jankhunter/okhttp3/JankHunterOkHttp3"
        private const val REAL_CONNECTION = "okhttp3/internal/connection/RealConnection"
        private const val HTTP2_CONNECTION = "okhttp3/internal/http2/Http2Connection"
        private const val HTTP2_CODEC = "okhttp3/internal/http2/Http2Codec"
        private const val HTTP2_STREAM = "okhttp3/internal/http2/Http2Stream"
        private const val STREAM_ALLOCATION = "okhttp3/internal/connection/StreamAllocation"
        private const val REAL_CALL = "okhttp3/RealCall"
        private const val CACHE_BODY = "okhttp3/Cache\$CacheResponseBody"
        private const val STATE_FIELD = "__jankHunterTransportV1"
        private const val STATE_ACCESS = "jankHunterTransportState"

        fun matches(className: String): Boolean = when (className) {
            "okhttp3.internal.connection.RealConnection", "okhttp3.internal.http2.Http2Connection",
            "okhttp3.internal.http2.Http2Codec", "okhttp3.RealCall", "okhttp3.Cache\$CacheResponseBody" -> true
            else -> false
        }
    }
}
