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
                action = { emitter, _ -> emitter.guardOkHttpNewCall() },
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
            is HookIntent.ExecutorRunnable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.wrapExecutorRunnable(intent.kind) },
            )
            is HookIntent.ExecutorCallable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.wrapExecutorCallable(intent.kind) },
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

    fun guardOkHttpNewCall() {
        invokeOkHttpHelper(
            "newCall",
            "(Lokhttp3/OkHttpClient;Lokhttp3/Request;)Lokhttp3/Call;",
        )
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

    private fun loadInvocation(saved: SavedInvocation) {
        visitor.loadLocal(saved.receiverLocal)
        saved.argumentTypes.indices.forEach { index ->
            visitor.loadLocal(saved.argumentLocals[index], saved.argumentTypes[index])
        }
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

    fun wrapExecutorRunnable(kind: ExecutorRunnableKind) {
        when (kind) {
            ExecutorRunnableKind.SINGLE_RUNNABLE -> wrapTopRunnable()
            ExecutorRunnableKind.RUNNABLE_OBJECT -> wrapRunnableBeforeObject()
            ExecutorRunnableKind.RUNNABLE_LONG_OBJECT -> wrapRunnableBeforeLongAndObject()
            ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT -> wrapRunnableBeforeTwoLongsAndObject()
        }
    }

    fun wrapExecutorCallable(kind: ExecutorCallableKind) {
        when (kind) {
            ExecutorCallableKind.SINGLE_CALLABLE -> wrapTopCallable()
            ExecutorCallableKind.CALLABLE_LONG_OBJECT -> wrapCallableBeforeLongAndObject()
        }
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

    private fun wrapTopRunnable() =
        wrapTop("wrapRunnable", "(Ljava/lang/Runnable;Ljava/lang/String;)Ljava/lang/Runnable;")

    private fun wrapTopCallable() =
        wrapTop("wrapCallable", "(Ljava/util/concurrent/Callable;Ljava/lang/String;)Ljava/util/concurrent/Callable;")

    private fun wrapTopCoroutineBlock() =
        wrapTop(
            "wrapCoroutineBlock",
            "(Lkotlin/jvm/functions/Function2;Ljava/lang/String;)Lkotlin/jvm/functions/Function2;",
        )

    private fun wrapRunnableBeforeObject() {
        val objectLocal = visitor.newLocal(OBJECT_TYPE)
        visitor.storeLocal(objectLocal)
        wrapTopRunnable()
        visitor.loadLocal(objectLocal)
    }

    private fun wrapRunnableBeforeLongAndObject() {
        val objectLocal = visitor.newLocal(OBJECT_TYPE)
        val delayLocal = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(objectLocal)
        visitor.storeLocal(delayLocal)
        wrapTopRunnable()
        visitor.loadLocal(delayLocal)
        visitor.loadLocal(objectLocal)
    }

    private fun wrapRunnableBeforeTwoLongsAndObject() {
        val objectLocal = visitor.newLocal(OBJECT_TYPE)
        val secondLongLocal = visitor.newLocal(Type.LONG_TYPE)
        val firstLongLocal = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(objectLocal)
        visitor.storeLocal(secondLongLocal)
        visitor.storeLocal(firstLongLocal)
        wrapTopRunnable()
        visitor.loadLocal(firstLongLocal)
        visitor.loadLocal(secondLongLocal)
        visitor.loadLocal(objectLocal)
    }

    private fun wrapCallableBeforeLongAndObject() {
        val objectLocal = visitor.newLocal(OBJECT_TYPE)
        val delayLocal = visitor.newLocal(Type.LONG_TYPE)
        visitor.storeLocal(objectLocal)
        visitor.storeLocal(delayLocal)
        wrapTopCallable()
        visitor.loadLocal(delayLocal)
        visitor.loadLocal(objectLocal)
    }

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
