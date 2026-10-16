package io.jankhunter.gradle

import java.util.function.LongSupplier
import io.jankhunter.sql.SqlNormalizer
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter

internal interface BytecodeCommand {
    val id: String
    val replacesOriginalCall: Boolean
    fun emit(emitter: HookBytecodeEmitter, invocation: MethodInvocation)
}

internal data class MethodInvocation(
    val opcodeAndSource: Int,
    val owner: String,
    val name: String,
    val descriptor: String,
    val isInterface: Boolean,
)

internal object BytecodeCommandFactory {
    fun commandFor(intent: HookIntent): BytecodeCommand {
        return when (intent) {
            HookIntent.WrapOkHttpEventListenerFactory -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.wrapOkHttpEventListenerFactory() },
            )
            HookIntent.InstallOkHttpEventListener -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, _ -> emitter.installOkHttpEventListener() },
            )
            HookIntent.InstallOkHttpEventListenerFactory -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.installOkHttpEventListenerFactory() },
            )
            HookIntent.GuardOkHttpNewCall -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.guardOkHttpNewCall(invocation) },
            )
            HookIntent.WrapWebSocketListener -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, invocation -> emitter.wrapWebSocketListener(invocation) },
            )
            is HookIntent.HandlerRunnable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.postHandlerRunnable(invocation) },
            )
            is HookIntent.HandlerRemoveCallbacks -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.preserveInvocation(invocation) },
            )
            HookIntent.HandlerRemoveCallbacksAndMessages -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.preserveInvocation(invocation) },
            )
            HookIntent.HandlerHasCallbacks -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.preserveInvocation(invocation) },
            )
            HookIntent.HandlerMessageSend -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.recordCallCounter("handler.send_message") },
            )
            HookIntent.WorkSubmission -> SimpleCommand(
                id = intent.id, replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.observeWorkSubmission(invocation) },
            )
            HookIntent.WorkBuilderBackoff, HookIntent.WorkBuilderBackoffDuration,
            HookIntent.WorkBuilderExpedited, HookIntent.WorkBuilderBuild -> SimpleCommand(
                id = intent.id, replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.observeWorkBuilder(intent, invocation) },
            )
            HookIntent.WorkChainRoot -> SimpleCommand(
                id = intent.id, replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.observeWorkChain(invocation, root = true) },
            )
            HookIntent.WorkChainChild -> SimpleCommand(
                id = intent.id, replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.observeWorkChain(invocation, root = false) },
            )
            HookIntent.WorkChainCombine -> SimpleCommand(
                id = intent.id, replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.observeWorkChainCombine(invocation) },
            )
            HookIntent.ThreadStart -> SimpleCommand(
                id = intent.id, replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.observeThreadStart(invocation) },
            )
            HookIntent.ThreadFactoryReturn -> SimpleCommand(
                id = intent.id, replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.observeThreadFactory(invocation) },
            )
            is HookIntent.ExecutorRunnable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, invocation -> emitter.wrapExecutorTask(invocation, callable = false) },
            )
            is HookIntent.ExecutorCallable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, invocation -> emitter.wrapExecutorTask(invocation, callable = true) },
            )
            is HookIntent.CoroutineBlock -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.wrapCoroutineBlock(intent.kind) },
            )
            HookIntent.WrapClickListener -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.wrapTopClickListener() },
            )
            is HookIntent.LogSpam -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.recordLogSpam(intent.source, intent.level) },
            )
            is HookIntent.CriticalIO -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, _ -> emitter.criticalIO(intent.kind) },
            )
            is HookIntent.DatabaseCall -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.databaseCall(intent, invocation) },
            )
            is HookIntent.DatabaseTransaction -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.databaseTransaction(intent, invocation) },
            )
        }
    }
}

private data class SimpleCommand(
    override val id: String,
    override val replacesOriginalCall: Boolean,
    private val action: (HookBytecodeEmitter, MethodInvocation) -> Unit,
) : BytecodeCommand {
    override fun emit(emitter: HookBytecodeEmitter, invocation: MethodInvocation) {
        action(emitter, invocation)
    }
}

internal class HookBytecodeEmitter(
    private val visitor: AdviceAdapter,
    private val ownerLabel: () -> String,
    private val ownerId: LongSupplier,
    private val emitOriginal: (MethodInvocation) -> Unit,
    private val emitTryCatchBlock: (org.objectweb.asm.Label, org.objectweb.asm.Label, org.objectweb.asm.Label, String?) -> Unit,
    private val sourceLabel: () -> String = ownerLabel,
) {
    fun databaseTransaction(intent: HookIntent.DatabaseTransaction, invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        if (intent.action != DatabaseTransactionAction.END) {
            loadInvocation(saved)
            emitOriginal(invocation)
            emitDatabaseTransactionHook(intent, saved.receiverLocal, throwableLocal = -1)
            return
        }

        val throwableLocal = visitor.newLocal(THROWABLE_TYPE)
        val tryStart = org.objectweb.asm.Label()
        val tryEnd = org.objectweb.asm.Label()
        val catchHandler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        emitTryCatchBlock(tryStart, tryEnd, catchHandler, null)
        visitor.visitLabel(tryStart)
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.visitLabel(tryEnd)
        emitDatabaseTransactionHook(intent, saved.receiverLocal, throwableLocal = -1)
        visitor.goTo(done)
        visitor.visitLabel(catchHandler)
        visitor.storeLocal(throwableLocal)
        emitDatabaseTransactionHook(intent, saved.receiverLocal, throwableLocal)
        visitor.loadLocal(throwableLocal)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    private fun emitDatabaseTransactionHook(
        intent: HookIntent.DatabaseTransaction,
        receiverLocal: Int,
        throwableLocal: Int,
    ) {
        visitor.loadLocal(receiverLocal)
        when (intent.action) {
            DatabaseTransactionAction.BEGIN -> {
                visitor.visitLdcInsn(ownerId.asLong)
                visitor.visitLdcInsn(ownerLabel())
                visitor.push(intent.mode.wireValue)
                invokeHook("beginDatabaseTransaction", "(Ljava/lang/Object;JLjava/lang/String;I)V")
            }
            DatabaseTransactionAction.MARK_SUCCESSFUL -> {
                invokeHook("markDatabaseTransactionSuccessful", "(Ljava/lang/Object;)V")
            }
            DatabaseTransactionAction.END -> {
                if (throwableLocal >= 0) {
                    visitor.loadLocal(throwableLocal)
                } else {
                    visitor.visitInsn(Opcodes.ACONST_NULL)
                }
                invokeHook("endDatabaseTransaction", "(Ljava/lang/Object;Ljava/lang/Throwable;)V")
            }
        }
    }

    fun databaseCall(intent: HookIntent.DatabaseCall, invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        val preparedLocal = emitPreparedStatementResolution(intent, saved)
        val queryLocal = emitRuntimeDatabaseQuery(intent, saved, preparedLocal)
        val fingerprintLocal = emitRuntimeDatabaseFingerprint(queryLocal, preparedLocal)
        if (intent.statementAction == DatabaseStatementAction.REGISTER) {
            loadInvocation(saved)
            emitOriginal(invocation)
            emitPreparedStatementRegistration(
                intent,
                queryLocal,
                fingerprintLocal,
                SqlNormalizer.fingerprint(intent.query),
            )
            return
        }
        val statementTokenLocal = emitPreparedStatementToken(preparedLocal)
        val operationLocal = emitRuntimeDatabaseOperation(intent, queryLocal)
        val staticFingerprint = SqlNormalizer.fingerprint(intent.query)
        val staticOperation = SqlNormalizer.operation(intent.query, intent.operation.wireValue)
        invokeHook("enterDatabase", "()J")
        val tokenLocal = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(tokenLocal)

        val throwableLocal = visitor.newLocal(THROWABLE_TYPE)
        val tryStart = org.objectweb.asm.Label()
        val tryEnd = org.objectweb.asm.Label()
        val catchHandler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        emitTryCatchBlock(tryStart, tryEnd, catchHandler, null)

        visitor.visitLabel(tryStart)
        loadInvocation(saved)
        emitOriginal(invocation)
        emitPreparedStatementRegistration(intent, queryLocal, fingerprintLocal, staticFingerprint)
        val resultBucketLocal = emitDatabaseResultBucket(intent, invocation)
        visitor.visitLabel(tryEnd)
        emitDatabaseExit(
            tokenLocal, queryLocal, fingerprintLocal, operationLocal,
            statementTokenLocal, resultBucketLocal, staticFingerprint, staticOperation,
            intent, succeeded = true, throwableLocal = -1,
        )
        visitor.goTo(done)

        visitor.visitLabel(catchHandler)
        visitor.storeLocal(throwableLocal)
        emitDatabaseExit(
            tokenLocal, queryLocal, fingerprintLocal, operationLocal,
            statementTokenLocal, resultBucketLocal = -1, staticFingerprint, staticOperation,
            intent, succeeded = false, throwableLocal = throwableLocal,
        )
        visitor.loadLocal(throwableLocal)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    private fun emitPreparedStatementResolution(intent: HookIntent.DatabaseCall, saved: SavedInvocation): Int {
        if (intent.statementAction != DatabaseStatementAction.EXECUTE) return -1
        visitor.loadLocal(saved.receiverLocal)
        invokeHook("resolvePreparedStatement", "(Ljava/lang/Object;)Ljava/lang/Object;")
        return visitor.newLocal(OBJECT_TYPE).also(visitor::storeLocal)
    }

    private fun emitRuntimeDatabaseQuery(
        intent: HookIntent.DatabaseCall,
        saved: SavedInvocation,
        preparedLocal: Int,
    ): Int {
        if (intent.query != null) return -1
        if (preparedLocal >= 0) {
            visitor.loadLocal(preparedLocal)
            invokeHook("preparedStatementQuery", "(Ljava/lang/Object;)Ljava/lang/String;")
            return visitor.newLocal(STRING_TYPE).also(visitor::storeLocal)
        }
        val argument = intent.queryArgument ?: return -1
        if (argument !in saved.argumentLocals.indices) return -1
        visitor.loadLocal(saved.argumentLocals[argument], saved.argumentTypes[argument])
        invokeHook("normalizeDatabaseQuery", "(Ljava/lang/String;)Ljava/lang/String;")
        return visitor.newLocal(STRING_TYPE).also(visitor::storeLocal)
    }

    private fun emitRuntimeDatabaseOperation(intent: HookIntent.DatabaseCall, queryLocal: Int): Int {
        if (queryLocal < 0) return -1
        visitor.loadLocal(queryLocal)
        visitor.push(intent.operation.wireValue)
        invokeHook("databaseQueryOperation", "(Ljava/lang/String;I)I")
        return visitor.newLocal(Type.INT_TYPE).also(visitor::storeLocal)
    }

    private fun emitRuntimeDatabaseFingerprint(queryLocal: Int, preparedLocal: Int): Int {
        if (preparedLocal >= 0) {
            visitor.loadLocal(preparedLocal)
            invokeHook("preparedStatementFingerprint", "(Ljava/lang/Object;)J")
            return visitor.newLocal(Type.LONG_TYPE).also(visitor::storeLocal)
        }
        if (queryLocal < 0) return -1
        visitor.loadLocal(queryLocal)
        invokeHook("databaseStatementFingerprint", "(Ljava/lang/String;)J")
        return visitor.newLocal(Type.LONG_TYPE).also(visitor::storeLocal)
    }

    private fun emitPreparedStatementToken(preparedLocal: Int): Int {
        if (preparedLocal < 0) return -1
        visitor.loadLocal(preparedLocal)
        invokeHook("preparedStatementToken", "(Ljava/lang/Object;)J")
        return visitor.newLocal(Type.LONG_TYPE).also(visitor::storeLocal)
    }

    private fun emitPreparedStatementRegistration(
        intent: HookIntent.DatabaseCall,
        queryLocal: Int,
        fingerprintLocal: Int,
        staticFingerprint: Long,
    ) {
        if (intent.statementAction != DatabaseStatementAction.REGISTER) return
        visitor.dup()
        when {
            intent.query != null -> visitor.visitLdcInsn(intent.query)
            queryLocal >= 0 -> visitor.loadLocal(queryLocal)
            else -> visitor.visitInsn(Opcodes.ACONST_NULL)
        }
        if (fingerprintLocal >= 0) visitor.loadLocal(fingerprintLocal) else visitor.visitLdcInsn(staticFingerprint)
        invokeHook("registerPreparedStatement", "(Ljava/lang/Object;Ljava/lang/String;J)V")
    }

    private fun emitDatabaseResultBucket(intent: HookIntent.DatabaseCall, invocation: MethodInvocation): Int {
        if (intent.resultCapture == DatabaseResultCapture.NONE) return -1
        when (Type.getReturnType(invocation.descriptor)) {
            Type.INT_TYPE -> {
                visitor.dup()
                visitor.visitInsn(Opcodes.I2L)
            }
            Type.LONG_TYPE -> visitor.dup2()
            else -> return -1
        }
        visitor.push(intent.resultCapture.wireValue)
        invokeHook("databaseResultCountBucket", "(JI)I")
        return visitor.newLocal(Type.INT_TYPE).also(visitor::storeLocal)
    }

    private fun emitDatabaseExit(
        tokenLocal: Int,
        queryLocal: Int,
        fingerprintLocal: Int,
        operationLocal: Int,
        statementTokenLocal: Int,
        resultBucketLocal: Int,
        staticFingerprint: Long,
        staticOperation: Int,
        intent: HookIntent.DatabaseCall,
        succeeded: Boolean,
        throwableLocal: Int,
    ) {
        visitor.loadLocal(tokenLocal)
        visitor.visitLdcInsn(ownerId.asLong)
        visitor.visitLdcInsn(ownerLabel())
        when {
            intent.query != null -> visitor.visitLdcInsn(intent.query)
            queryLocal >= 0 -> visitor.loadLocal(queryLocal)
            else -> visitor.visitInsn(Opcodes.ACONST_NULL)
        }
        if (fingerprintLocal >= 0) visitor.loadLocal(fingerprintLocal) else visitor.visitLdcInsn(staticFingerprint)
        visitor.push(intent.framework.wireValue)
        if (operationLocal >= 0) visitor.loadLocal(operationLocal) else visitor.push(staticOperation)
        visitor.push(intent.boundary.wireValue)
        visitor.push(succeeded && resultBucketLocal >= 0)
        visitor.push(if (succeeded && resultBucketLocal >= 0) intent.resultCapture.resultKindWireValue else 0)
        if (succeeded && resultBucketLocal >= 0) visitor.loadLocal(resultBucketLocal) else visitor.push(0)
        if (statementTokenLocal >= 0) visitor.loadLocal(statementTokenLocal) else visitor.visitLdcInsn(0L)
        visitor.push(succeeded)
        if (throwableLocal >= 0) visitor.loadLocal(throwableLocal) else visitor.visitInsn(Opcodes.ACONST_NULL)
        invokeHook("exitDatabase", "(JJLjava/lang/String;Ljava/lang/String;JIIIZIIJZLjava/lang/Throwable;)V")
    }

    fun criticalIO(kind: CriticalIOCallKind) {
        visitor.visitLdcInsn(ownerId.asLong)
        visitor.visitLdcInsn(ownerLabel())
        val (name, descriptor) = when (kind) {
            CriticalIOCallKind.FILE_READ_BYTES ->
                "readFileBytes" to "(Ljava/io/File;JLjava/lang/String;)[B"
            CriticalIOCallKind.FILE_WRITE_BYTES ->
                "writeFileBytes" to "(Ljava/io/File;[BJLjava/lang/String;)V"
            CriticalIOCallKind.FILE_APPEND_BYTES ->
                "appendFileBytes" to "(Ljava/io/File;[BJLjava/lang/String;)V"
            CriticalIOCallKind.FILE_DESCRIPTOR_SYNC ->
                "syncFileDescriptor" to "(Ljava/io/FileDescriptor;JLjava/lang/String;)V"
            CriticalIOCallKind.FILE_CHANNEL_FORCE ->
                "forceFileChannel" to "(Ljava/nio/channels/FileChannel;ZJLjava/lang/String;)V"
        }
        visitor.visitMethodInsn(Opcodes.INVOKESTATIC, JANK_HUNTER_IO_HOOKS, name, descriptor, false)
    }

    fun wrapOkHttpEventListenerFactory() {
        invokeOkHttpHelper(
            "wrapEventListenerFactory",
            "(Lokhttp3/EventListener\$Factory;)Lokhttp3/EventListener\$Factory;",
        )
    }

    fun installOkHttpEventListener() {
        invokeOkHttpHelper(
            "installEventListener",
            "(Lokhttp3/OkHttpClient\$Builder;Lokhttp3/EventListener;)Lokhttp3/OkHttpClient\$Builder;",
        )
    }

    fun installOkHttpEventListenerFactory() {
        invokeOkHttpHelper(
            "installEventListenerFactory",
            "(Lokhttp3/OkHttpClient\$Builder;)Lokhttp3/OkHttpClient\$Builder;",
        )
    }

    fun guardOkHttpNewCall(invocation: MethodInvocation) {
        // A virtual helper cannot express invokespecial: redispatching can recurse into the
        // application's override instead of calling the explicitly selected superclass method.
        if (invocation.opcodeAndSource == Opcodes.INVOKESPECIAL) {
            emitOriginal(invocation)
            return
        }
        val saved = saveInstanceInvocation(invocation)
        val original = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.loadLocal(saved.receiverLocal)
        visitor.ifNull(original)
        visitor.loadLocal(saved.argumentLocals[0])
        visitor.ifNull(original)
        visitor.loadLocal(saved.receiverLocal)
        visitor.visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/lang/Object", "getClass", "()Ljava/lang/Class;", false)
        visitor.visitLdcInsn(Type.getObjectType("okhttp3/OkHttpClient"))
        visitor.visitJumpInsn(Opcodes.IF_ACMPNE, original)
        // Kotlin helper nullability is stricter than an arbitrary Java override. Only the exact
        // reviewed base client with non-null arguments can enter its replacement ABI.
        loadInvocation(saved)
        invokeOkHttpHelper(
            "newCall",
            "(Lokhttp3/OkHttpClient;Lokhttp3/Request;)Lokhttp3/Call;",
        )
        visitor.goTo(done)
        visitor.visitLabel(original)
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.visitLabel(done)
    }

    fun wrapWebSocketListener(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(saved.argumentLocals[0], saved.argumentTypes[0])
        visitor.loadLocal(saved.argumentLocals[0], saved.argumentTypes[0])
        visitor.loadLocal(saved.argumentLocals[1], saved.argumentTypes[1])
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            OKHTTP_HELPERS,
            "wrapWebSocketListener",
            "(Lokhttp3/Request;Lokhttp3/WebSocketListener;Ljava/lang/String;)Lokhttp3/WebSocketListener;",
            false,
        )
    }

    fun postHandlerRunnable(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        val originalRunnable = saved.argumentLocals[0]
        val resultLocal = visitor.newLocal(Type.BOOLEAN_TYPE)
        // Preserve identity and the single platform call, including its return value and exceptions.
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.storeLocal(resultLocal)
        visitor.loadLocal(originalRunnable)
        visitor.loadLocal(originalRunnable)
        visitor.loadLocal(resultLocal)
        invokeHook("onHandlerPostResult", "(Ljava/lang/Runnable;Ljava/lang/Runnable;Z)V")
        visitor.loadLocal(resultLocal)
    }

    fun causalHandlerPost(invocation: MethodInvocation, site: CausalSourceSite, legacyEnabled: Boolean) {
        val saved = saveInstanceInvocation(invocation)
        val runnable = saved.argumentLocals[0]
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(runnable)
        emitCausalSite(site)
        if (invocation.name == "postDelayed") {
            val delayIndex = saved.argumentTypes.indexOfLast { it == Type.LONG_TYPE }
            check(delayIndex >= 0) { "postDelayed bridge requires a delay argument" }
            visitor.loadLocal(saved.argumentLocals[delayIndex], Type.LONG_TYPE)
        } else {
            // postAtTime accepts an absolute uptime deadline, not a requested delay.
            visitor.visitLdcInsn(Long.MIN_VALUE)
        }
        invokeHook("beginHandlerPost", "(Ljava/lang/Object;Ljava/lang/Runnable;JLjava/lang/String;IIJ)J")
        val token = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(token)
        val start = org.objectweb.asm.Label()
        val end = org.objectweb.asm.Label()
        val handler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.visitLabel(start)
        loadInvocation(saved)
        emitOriginal(invocation)
        val posted = visitor.newLocal(Type.BOOLEAN_TYPE)
        visitor.storeLocal(posted)
        visitor.visitLabel(end)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(runnable)
        visitor.loadLocal(token)
        visitor.loadLocal(posted)
        invokeHook("handlerPostReturned", "(Ljava/lang/Object;Ljava/lang/Runnable;JZ)V")
        if (legacyEnabled) {
            visitor.loadLocal(runnable)
            visitor.loadLocal(runnable)
            visitor.loadLocal(posted)
            invokeHook("onHandlerPostResult", "(Ljava/lang/Runnable;Ljava/lang/Runnable;Z)V")
        }
        visitor.loadLocal(posted)
        visitor.goTo(done)
        emitTryCatchBlock(start, end, handler, "java/lang/Throwable")
        visitor.visitLabel(handler)
        val thrown = visitor.newLocal(Type.getType(Throwable::class.java))
        visitor.storeLocal(thrown)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(runnable)
        visitor.loadLocal(token)
        invokeHook("handlerPostThrew", "(Ljava/lang/Object;Ljava/lang/Runnable;J)V")
        visitor.loadLocal(thrown)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun causalTrackedHandlerPost(invocation: MethodInvocation, site: CausalSourceSite) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitCausalSite(site)
        val descriptor = invocation.descriptor.replace(
            ")Lio/jankhunter/runtime/JankHunterPostHandle;",
            "JLjava/lang/String;II)Lio/jankhunter/runtime/JankHunterPostHandle;",
        )
        check(descriptor != invocation.descriptor) { "Unsupported tracked Handler signature" }
        emitOriginal(invocation.copy(descriptor = descriptor))
    }

    fun causalResourceCall(invocation: MethodInvocation, site: CausalSourceSite) {
        val resourceOwner = "io/jankhunter/runtime/JankHunterResources"
        if (invocation.name in setOf("withLock", "withMonitor", "use")) {
            val saved = saveInstanceInvocation(invocation)
            // The original Kotlin object API is INSTANCE.method(...), whereas the metadata
            // overloads are @JvmStatic. Drop only the singleton receiver after saving arguments.
            saved.argumentTypes.indices.forEach { index ->
                visitor.loadLocal(saved.argumentLocals[index], saved.argumentTypes[index])
            }
            emitCausalSite(site)
            visitor.visitMethodInsn(
                Opcodes.INVOKESTATIC,
                resourceOwner,
                invocation.name,
                invocation.descriptor.replace(")", "JLjava/lang/String;II)"),
                false,
            )
            return
        }
        if (invocation.name == "beginAcquire\$default") {
            // Kotlin's generated default bridge has an unused marker as its last argument.
            // The causal bridge implements the same mask rule without dropping provenance.
            visitor.pop()
            emitCausalSite(site)
            visitor.visitMethodInsn(
                Opcodes.INVOKESTATIC,
                resourceOwner,
                "beginAcquireWithDefaults",
                "(Lio/jankhunter/runtime/JankHunterResourceKind;ZIJLjava/lang/String;II)" +
                    "Lio/jankhunter/runtime/JankHunterResourceLease;",
                false,
            )
            return
        }
        if (invocation.name == "beginAcquire" && invocation.descriptor ==
            "(Lio/jankhunter/runtime/JankHunterResourceKind;)" +
            "Lio/jankhunter/runtime/JankHunterResourceLease;") {
            visitor.push(false)
        }
        emitCausalSite(site)
        val descriptor = if (invocation.name == "beginAcquire") {
            "(Lio/jankhunter/runtime/JankHunterResourceKind;ZJLjava/lang/String;II)" +
                "Lio/jankhunter/runtime/JankHunterResourceLease;"
        } else {
            invocation.descriptor.replace(")", "JLjava/lang/String;II)")
        }
        emitOriginal(invocation.copy(descriptor = descriptor))
    }

    private fun saveInstanceInvocation(invocation: MethodInvocation): SavedInvocation {
        val argumentTypes = Type.getArgumentTypes(invocation.descriptor)
        val argumentLocals = IntArray(argumentTypes.size)
        for (index in argumentTypes.indices.reversed()) {
            val type = argumentTypes[index]
            argumentLocals[index] = visitor.newLocal(type)
            visitor.storeLocal(argumentLocals[index], type)
        }
        val receiverType = Type.getObjectType(invocation.owner)
        val receiverLocal = visitor.newLocal(receiverType)
        visitor.storeLocal(receiverLocal, receiverType)
        return SavedInvocation(receiverLocal, argumentLocals, argumentTypes)
    }

    private fun saveStaticInvocation(invocation: MethodInvocation): SavedInvocation {
        val argumentTypes = Type.getArgumentTypes(invocation.descriptor)
        val argumentLocals = IntArray(argumentTypes.size)
        for (index in argumentTypes.indices.reversed()) {
            val type = argumentTypes[index]
            argumentLocals[index] = visitor.newLocal(type)
            visitor.storeLocal(argumentLocals[index], type)
        }
        return SavedInvocation(-1, argumentLocals, argumentTypes)
    }

    private fun loadInvocation(saved: SavedInvocation) {
        if (saved.receiverLocal >= 0) visitor.loadLocal(saved.receiverLocal)
        saved.argumentTypes.indices.forEach { index ->
            visitor.loadLocal(saved.argumentLocals[index], saved.argumentTypes[index])
        }
    }

    fun observeWorkSubmission(invocation: MethodInvocation, causalSite: CausalSourceSite? = null) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.getReturnType(invocation.descriptor))
        visitor.storeLocal(result)
        WorkSubmissionBytecode(visitor, sourceLabel(), causalSite).emit(result, saved.argumentLocals)
        visitor.loadLocal(result)
    }

    fun observeWorkBuilder(intent: HookIntent, invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.getReturnType(invocation.descriptor))
        visitor.storeLocal(result)
        visitor.loadLocal(saved.receiverLocal)
        when (intent) {
            HookIntent.WorkBuilderBackoff -> {
                visitor.loadLocal(saved.argumentLocals[0])
                visitor.loadLocal(saved.argumentLocals[1], Type.LONG_TYPE)
                visitor.loadLocal(saved.argumentLocals[2])
                invokeHook("observeWorkBuilderBackoffCall", "(Ljava/lang/Object;Ljava/lang/Object;JLjava/lang/Object;)V")
            }
            HookIntent.WorkBuilderBackoffDuration -> {
                visitor.loadLocal(saved.argumentLocals[0])
                visitor.loadLocal(saved.argumentLocals[1])
                invokeHook("observeWorkBuilderBackoffDurationCall", "(Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;)V")
            }
            HookIntent.WorkBuilderExpedited -> {
                visitor.loadLocal(saved.argumentLocals[0])
                invokeHook("observeWorkBuilderExpeditedCall", "(Ljava/lang/Object;Ljava/lang/Object;)V")
            }
            HookIntent.WorkBuilderBuild -> {
                visitor.loadLocal(result)
                invokeHook("observeWorkBuilderBuildCall", "(Ljava/lang/Object;Ljava/lang/Object;)V")
            }
            else -> error("unsupported builder hook")
        }
        visitor.loadLocal(result)
    }

    fun observeWorkChain(invocation: MethodInvocation, root: Boolean) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.getReturnType(invocation.descriptor))
        visitor.storeLocal(result)
        WorkChainBytecode(visitor, sourceLabel()).emit(
            saved.receiverLocal, result, saved.argumentLocals, root,
        )
        visitor.loadLocal(result)
    }

    fun observeWorkChainCombine(invocation: MethodInvocation) {
        val parents = visitor.newLocal(Type.getType("Ljava/util/List;"))
        visitor.storeLocal(parents)
        visitor.loadLocal(parents)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.getReturnType(invocation.descriptor))
        visitor.storeLocal(result)
        WorkChainBytecode(visitor, sourceLabel()).emitCombine(parents, result)
        visitor.loadLocal(result)
    }

    fun observeThreadStart(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.loadLocal(saved.receiverLocal)
        visitor.visitLdcInsn(sourceLabel())
        visitor.visitMethodInsn(Opcodes.INVOKESTATIC, JANK_HUNTER_HOOKS, "observeThreadStart", "(Ljava/lang/Thread;Ljava/lang/String;)V", false)
    }

    fun observeThreadFactory(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.getReturnType(invocation.descriptor))
        visitor.storeLocal(result)
        visitor.loadLocal(result)
        visitor.loadLocal(saved.receiverLocal)
        visitor.visitLdcInsn(sourceLabel())
        visitor.visitMethodInsn(Opcodes.INVOKESTATIC, JANK_HUNTER_HOOKS, "observeThreadFactory",
            "(Ljava/lang/Thread;Ljava/lang/Object;Ljava/lang/String;)V", false)
        visitor.loadLocal(result)
    }

    fun causalThreadConstructed(
        invocation: MethodInvocation,
        site: CausalSourceSite,
        controlledTarget: Boolean,
    ) {
        val saved = saveInstanceInvocation(invocation)
        val targetIndex = if (controlledTarget && invocation.owner == "java/lang/Thread") {
            saved.argumentTypes.indexOfFirst { it.descriptor == "Ljava/lang/Runnable;" }
        } else {
            -1
        }
        if (targetIndex >= 0) {
            visitor.loadLocal(saved.argumentLocals[targetIndex], saved.argumentTypes[targetIndex])
            emitCausalSite(site)
            invokeHook("prepareThreadTarget", "(Ljava/lang/Runnable;JLjava/lang/String;II)Ljava/lang/Runnable;")
            val prepared = visitor.newLocal(Type.getType(Runnable::class.java))
            visitor.storeLocal(prepared)
            visitor.loadLocal(saved.receiverLocal)
            saved.argumentTypes.indices.forEach { index ->
                if (index == targetIndex) visitor.loadLocal(prepared)
                else visitor.loadLocal(saved.argumentLocals[index], saved.argumentTypes[index])
            }
            emitOriginal(invocation)
            visitor.loadLocal(saved.receiverLocal)
            visitor.loadLocal(prepared)
            invokeHook("bindThreadTarget", "(Ljava/lang/Thread;Ljava/lang/Runnable;)V")
            // A null target is legal. bindThreadTarget has nothing to bind in that case, but
            // constructor provenance still exists; registry dedupes the non-null path.
            visitor.loadLocal(saved.receiverLocal)
            emitCausalSite(site)
            invokeHook("observeThreadConstructed", "(Ljava/lang/Thread;JLjava/lang/String;II)V")
            return
        }
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.loadLocal(saved.receiverLocal)
        emitCausalSite(site)
        invokeHook("observeThreadConstructed", "(Ljava/lang/Thread;JLjava/lang/String;II)V")
    }

    fun causalThreadFactory(invocation: MethodInvocation, site: CausalSourceSite) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.getReturnType(invocation.descriptor))
        visitor.storeLocal(result)
        visitor.loadLocal(result)
        visitor.loadLocal(saved.receiverLocal)
        emitCausalSite(site)
        invokeHook("observeThreadFactoryReturned", "(Ljava/lang/Thread;Ljava/lang/Object;JLjava/lang/String;II)V")
        visitor.loadLocal(result)
        visitor.loadLocal(saved.receiverLocal)
        visitor.visitLdcInsn(sourceLabel())
        invokeHook("observeThreadFactory", "(Ljava/lang/Thread;Ljava/lang/Object;Ljava/lang/String;)V")
        visitor.loadLocal(result)
    }

    fun causalThreadCall(invocation: MethodInvocation, site: CausalSourceSite, interrupt: Boolean) {
        val saved = saveInstanceInvocation(invocation)
        visitor.loadLocal(saved.receiverLocal)
        emitCausalSite(site)
        invokeHook(
            if (interrupt) "beginThreadInterrupt" else "beginThreadStart",
            "(Ljava/lang/Thread;JLjava/lang/String;II)J",
        )
        val token = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(token)
        val start = org.objectweb.asm.Label()
        val end = org.objectweb.asm.Label()
        val handler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.visitLabel(start)
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.visitLabel(end)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(token)
        invokeHook(
            if (interrupt) "threadInterruptReturned" else "threadStartReturned",
            "(Ljava/lang/Thread;J)V",
        )
        if (!interrupt) {
            visitor.loadLocal(saved.receiverLocal)
            visitor.visitLdcInsn(sourceLabel())
            invokeHook("observeThreadStart", "(Ljava/lang/Thread;Ljava/lang/String;)V")
        }
        visitor.goTo(done)
        emitTryCatchBlock(start, end, handler, "java/lang/Throwable")
        visitor.visitLabel(handler)
        val thrown = visitor.newLocal(Type.getType(Throwable::class.java))
        visitor.storeLocal(thrown)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(token)
        invokeHook(
            if (interrupt) "threadInterruptThrew" else "threadStartThrew",
            "(Ljava/lang/Thread;J)V",
        )
        visitor.loadLocal(thrown)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun causalThreadTerminationProbe(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        val resultType = Type.getReturnType(invocation.descriptor)
        val result = if (resultType != Type.VOID_TYPE) visitor.newLocal(resultType) else -1
        if (result >= 0) visitor.storeLocal(result, resultType)
        // join may time out and isAlive may be false for a never-started thread. The runtime
        // emits END_OBSERVED only after it sees the exact Thread.State.TERMINATED state.
        visitor.loadLocal(saved.receiverLocal)
        invokeHook("observeThreadEnd", "(Ljava/lang/Thread;)V")
        if (result >= 0) visitor.loadLocal(result, resultType)
    }

    fun causalExecutorHandoff(invocation: MethodInvocation, site: CausalSourceSite, callable: Boolean) {
        val saved = saveInstanceInvocation(invocation)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(saved.argumentLocals[0])
        visitor.visitLdcInsn(site.id)
        visitor.visitLdcInsn(site.method)
        visitor.visitLdcInsn(sourceLabel())
        visitor.push(site.line)
        visitor.push(site.origin.wireValue)
        when (invocation.name) {
            "schedule", "scheduleAtFixedRate", "scheduleWithFixedDelay" -> {
                visitor.loadLocal(saved.argumentLocals[1], Type.LONG_TYPE)
                if (invocation.name == "schedule") visitor.push(0L)
                else visitor.loadLocal(saved.argumentLocals[2], Type.LONG_TYPE)
                visitor.loadLocal(saved.argumentLocals.last())
                visitor.push(when (invocation.name) {
                    "scheduleAtFixedRate" -> 2
                    "scheduleWithFixedDelay" -> 3
                    else -> 1
                })
            }
            else -> {
                visitor.push(0L)
                visitor.push(0L)
                visitor.visitInsn(Opcodes.ACONST_NULL)
                visitor.push(0)
            }
        }
        invokeHook(
            if (callable) "prepareExecutorCallable" else "prepareExecutorRunnable",
            if (callable) {
                "(Ljava/lang/Object;Ljava/util/concurrent/Callable;JLjava/lang/String;Ljava/lang/String;IIJJLjava/lang/Object;I)" +
                    "Ljava/util/concurrent/Callable;"
            } else {
                "(Ljava/lang/Object;Ljava/lang/Runnable;JLjava/lang/String;Ljava/lang/String;IIJJLjava/lang/Object;I)" +
                    "Ljava/lang/Runnable;"
            },
        )
        visitor.storeLocal(saved.argumentLocals[0])
        val start = org.objectweb.asm.Label()
        val end = org.objectweb.asm.Label()
        val handler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.visitLabel(start)
        loadInvocation(saved)
        emitOriginal(invocation)
        val resultType = Type.getReturnType(invocation.descriptor)
        val result = if (resultType != Type.VOID_TYPE) visitor.newLocal(resultType) else -1
        if (result >= 0) visitor.storeLocal(result, resultType)
        visitor.visitLabel(end)
        visitor.loadLocal(saved.argumentLocals[0])
        if (result >= 0) visitor.loadLocal(result, resultType) else visitor.visitInsn(Opcodes.ACONST_NULL)
        invokeHook("executorHandoffReturned", "(Ljava/lang/Object;Ljava/lang/Object;)V")
        if (result >= 0) visitor.loadLocal(result, resultType)
        visitor.goTo(done)
        emitTryCatchBlock(start, end, handler, "java/lang/Throwable")
        visitor.visitLabel(handler)
        val thrown = visitor.newLocal(Type.getType(Throwable::class.java))
        visitor.storeLocal(thrown)
        visitor.loadLocal(saved.argumentLocals[0])
        invokeHook("executorHandoffThrew", "(Ljava/lang/Object;)V")
        visitor.loadLocal(thrown)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun causalCoroutineBlock(invocation: MethodInvocation, site: CausalSourceSite) {
        val saved = saveStaticInvocation(invocation)
        val blockIndex = saved.argumentTypes.indexOfLast {
            it.descriptor == "Lkotlin/jvm/functions/Function2;"
        }
        check(blockIndex >= 0) { "Matched coroutine builder without Function2 block" }
        val block = saved.argumentLocals[blockIndex]
        visitor.loadLocal(block)
        emitCausalSite(site)
        invokeHook(
            "wrapCausalCoroutineBlock",
            "(Lkotlin/jvm/functions/Function2;JLjava/lang/String;II)Lkotlin/jvm/functions/Function2;",
        )
        visitor.storeLocal(block)
        val jobReturn = invocation.name.startsWith("launch") || invocation.name.startsWith("async")
        val start = org.objectweb.asm.Label()
        val end = org.objectweb.asm.Label()
        val handler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.visitLabel(start)
        loadInvocation(saved)
        emitOriginal(invocation)
        val resultType = Type.getReturnType(invocation.descriptor)
        val result = visitor.newLocal(resultType)
        visitor.storeLocal(result, resultType)
        visitor.visitLabel(end)
        if (jobReturn) {
            visitor.loadLocal(block)
            visitor.loadLocal(result, resultType)
            invokeHook(
                "coroutineBuilderReturned",
                "(Lkotlin/jvm/functions/Function2;Ljava/lang/Object;)V",
            )
        }
        visitor.loadLocal(result, resultType)
        visitor.goTo(done)
        emitTryCatchBlock(start, end, handler, "java/lang/Throwable")
        visitor.visitLabel(handler)
        val thrown = visitor.newLocal(Type.getType(Throwable::class.java))
        visitor.storeLocal(thrown)
        if (jobReturn) {
            visitor.loadLocal(block)
            invokeHook("coroutineBuilderThrew", "(Lkotlin/jvm/functions/Function2;)V")
        }
        visitor.loadLocal(thrown)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun causalCoroutineJobCancel(invocation: MethodInvocation, site: CausalSourceSite) {
        val saved = if (invocation.opcodeAndSource and 0xff == Opcodes.INVOKESTATIC) {
            saveStaticInvocation(invocation)
        } else {
            saveInstanceInvocation(invocation)
        }
        val job = if (saved.receiverLocal >= 0) saved.receiverLocal else saved.argumentLocals[0]
        visitor.loadLocal(job)
        emitCausalSite(site)
        invokeHook("beginCoroutineJobCancel", "(Ljava/lang/Object;JLjava/lang/String;II)J")
        val token = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(token)
        val start = org.objectweb.asm.Label()
        val end = org.objectweb.asm.Label()
        val handler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.visitLabel(start)
        loadInvocation(saved)
        emitOriginal(invocation)
        val resultType = Type.getReturnType(invocation.descriptor)
        val result = if (resultType != Type.VOID_TYPE) visitor.newLocal(resultType) else -1
        if (result >= 0) visitor.storeLocal(result, resultType)
        visitor.visitLabel(end)
        visitor.loadLocal(job)
        visitor.loadLocal(token)
        invokeHook("coroutineJobCancelReturned", "(Ljava/lang/Object;J)V")
        if (result >= 0) visitor.loadLocal(result, resultType)
        visitor.goTo(done)
        emitTryCatchBlock(start, end, handler, "java/lang/Throwable")
        visitor.visitLabel(handler)
        val thrown = visitor.newLocal(Type.getType(Throwable::class.java))
        visitor.storeLocal(thrown)
        visitor.loadLocal(job)
        visitor.loadLocal(token)
        invokeHook("coroutineJobCancelThrew", "(Ljava/lang/Object;J)V")
        visitor.loadLocal(thrown)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun causalFutureCancel(invocation: MethodInvocation, site: CausalSourceSite) {
        val saved = saveInstanceInvocation(invocation)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(saved.argumentLocals[0], Type.BOOLEAN_TYPE)
        emitCausalSite(site)
        invokeHook("beginFutureCancel", "(Ljava/lang/Object;ZJLjava/lang/String;II)J")
        val token = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(token)
        val start = org.objectweb.asm.Label()
        val end = org.objectweb.asm.Label()
        val handler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.visitLabel(start)
        loadInvocation(saved)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.BOOLEAN_TYPE)
        visitor.storeLocal(result)
        visitor.visitLabel(end)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(token)
        visitor.loadLocal(result)
        invokeHook("futureCancelReturned", "(Ljava/lang/Object;JZ)V")
        visitor.loadLocal(result)
        visitor.goTo(done)
        emitTryCatchBlock(start, end, handler, "java/lang/Throwable")
        visitor.visitLabel(handler)
        val thrown = visitor.newLocal(Type.getType(Throwable::class.java))
        visitor.storeLocal(thrown)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(token)
        invokeHook("futureCancelThrew", "(Ljava/lang/Object;J)V")
        visitor.loadLocal(thrown)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun causalExecutorConstructed(invocation: MethodInvocation, site: CausalSourceSite) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.loadLocal(saved.receiverLocal)
        emitCausalSite(site)
        invokeHook("observeExecutorConstructed", "(Ljava/lang/Object;JLjava/lang/String;II)V")
    }

    fun causalExecutorFactory(invocation: MethodInvocation, site: CausalSourceSite) {
        emitOriginal(invocation)
        val resultType = Type.getReturnType(invocation.descriptor)
        val result = visitor.newLocal(resultType)
        visitor.storeLocal(result)
        visitor.loadLocal(result)
        emitCausalSite(site)
        invokeHook("observeExecutorFactoryReturned", "(Ljava/lang/Object;JLjava/lang/String;II)V")
        visitor.loadLocal(result)
    }

    fun causalExecutorShutdown(invocation: MethodInvocation, site: CausalSourceSite) {
        val saved = saveInstanceInvocation(invocation)
        visitor.loadLocal(saved.receiverLocal)
        visitor.push(invocation.name == "shutdownNow")
        emitCausalSite(site)
        invokeHook("beginExecutorShutdown", "(Ljava/lang/Object;ZJLjava/lang/String;II)J")
        val token = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(token)
        val start = org.objectweb.asm.Label()
        val end = org.objectweb.asm.Label()
        val handler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        visitor.visitLabel(start)
        loadInvocation(saved)
        emitOriginal(invocation)
        val resultType = Type.getReturnType(invocation.descriptor)
        val result = if (resultType != Type.VOID_TYPE) visitor.newLocal(resultType) else -1
        if (result >= 0) visitor.storeLocal(result, resultType)
        visitor.visitLabel(end)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(token)
        invokeHook("executorShutdownReturned", "(Ljava/lang/Object;J)V")
        if (result >= 0) visitor.loadLocal(result, resultType)
        visitor.goTo(done)
        emitTryCatchBlock(start, end, handler, "java/lang/Throwable")
        visitor.visitLabel(handler)
        val thrown = visitor.newLocal(Type.getType(Throwable::class.java))
        visitor.storeLocal(thrown)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(token)
        invokeHook("executorShutdownThrew", "(Ljava/lang/Object;J)V")
        visitor.loadLocal(thrown)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun causalExecutorTerminationCheck(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        val result = visitor.newLocal(Type.BOOLEAN_TYPE)
        visitor.storeLocal(result)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(result)
        invokeHook("observeExecutorTerminated", "(Ljava/lang/Object;Z)V")
        visitor.loadLocal(result)
    }

    private fun emitCausalSite(site: CausalSourceSite) {
        visitor.visitLdcInsn(site.id)
        visitor.visitLdcInsn(site.method)
        visitor.push(site.line)
        visitor.push(site.origin.wireValue)
    }

    fun preserveInvocation(invocation: MethodInvocation) = emitOriginal(invocation)

    private fun invokeHook(name: String, descriptor: String) {
        visitor.visitMethodInsn(Opcodes.INVOKESTATIC, JANK_HUNTER_HOOKS, name, descriptor, false)
    }

    private data class SavedInvocation(
        val receiverLocal: Int,
        val argumentLocals: IntArray,
        val argumentTypes: Array<Type>,
    )

    fun wrapExecutorTask(invocation: MethodInvocation, callable: Boolean) {
        val saved = saveInstanceInvocation(invocation)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(saved.argumentLocals[0])
        visitor.visitLdcInsn(sourceLabel())
        val taskType = if (callable) "Ljava/util/concurrent/Callable;" else "Ljava/lang/Runnable;"
        invokeHook(if (callable) "wrapCallable" else "wrapRunnable",
            "(Ljava/lang/Object;${taskType}Ljava/lang/String;)$taskType")
        visitor.storeLocal(saved.argumentLocals[0])
        loadInvocation(saved)
    }

    fun wrapCoroutineBlock(kind: CoroutineBlockKind) {
        when (kind) {
            CoroutineBlockKind.TOP_FUNCTION2 -> wrapTopCoroutineBlock()
            CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION -> wrapCoroutineBlockBeforeContinuation()
            CoroutineBlockKind.FUNCTION2_BEFORE_INT_OBJECT -> wrapCoroutineBlockBeforeIntAndObject()
        }
    }

    fun wrapTopClickListener() {
        wrapTop(
            "wrapClickListener",
            "(Landroid/view/View\$OnClickListener;Ljava/lang/String;)Landroid/view/View\$OnClickListener;",
        )
    }

    fun recordCallCounter(prefix: String) {
        visitor.visitLdcInsn("$prefix.count")
        visitor.visitInsn(Opcodes.LCONST_1)
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "recordCounter",
            "(Ljava/lang/String;J)V",
            false,
        )
    }

    fun recordLogSpam(source: String, level: Int) {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitLdcInsn(source)
        visitor.push(level)
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "recordLogSpam",
            "(Ljava/lang/String;Ljava/lang/String;I)V",
            false,
        )
    }

    private fun wrapTop(methodName: String, descriptor: String) {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            methodName,
            descriptor,
            false,
        )
    }

    private fun invokeOkHttpHelper(methodName: String, descriptor: String) {
        visitor.visitMethodInsn(Opcodes.INVOKESTATIC, OKHTTP_HELPERS, methodName, descriptor, false)
    }

    private fun wrapTopCoroutineBlock() =
        wrapTop(
            "wrapCoroutineBlock",
            "(Lkotlin/jvm/functions/Function2;Ljava/lang/String;)Lkotlin/jvm/functions/Function2;",
        )

    private fun wrapCoroutineBlockBeforeContinuation() {
        val continuationLocal = visitor.newLocal(OBJECT_TYPE)
        visitor.storeLocal(continuationLocal)
        wrapTopCoroutineBlock()
        visitor.loadLocal(continuationLocal)
    }

    private fun wrapCoroutineBlockBeforeIntAndObject() {
        val objectLocal = visitor.newLocal(OBJECT_TYPE)
        val maskLocal = visitor.newLocal(Type.INT_TYPE)
        visitor.storeLocal(objectLocal)
        visitor.storeLocal(maskLocal)
        wrapTopCoroutineBlock()
        visitor.loadLocal(maskLocal)
        visitor.loadLocal(objectLocal)
    }

    private companion object {
        private const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
        private const val OKHTTP_HELPERS = "io/jankhunter/okhttp3/JankHunterOkHttp3"
        private const val JANK_HUNTER_IO_HOOKS = "io/jankhunter/runtime/JankHunterIOHooks"
        private val OBJECT_TYPE: Type = Type.getType("Ljava/lang/Object;")
        private val STRING_TYPE: Type = Type.getType("Ljava/lang/String;")
        private val THROWABLE_TYPE: Type = Type.getType("Ljava/lang/Throwable;")
    }
}
