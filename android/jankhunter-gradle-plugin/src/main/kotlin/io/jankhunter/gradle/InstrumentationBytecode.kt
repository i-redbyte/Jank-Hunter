package io.jankhunter.gradle

import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter
import org.objectweb.asm.commons.GeneratorAdapter

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
            HookIntent.InstallOkHttpEventListenerFactory -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.installOkHttpEventListenerFactory() },
            )
            HookIntent.WrapWebSocketListener -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { emitter, _ -> emitter.wrapWebSocketListener() },
            )
            is HookIntent.HandlerRunnable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.postHandlerRunnable(intent.kind, invocation) },
            )
            is HookIntent.HandlerRemoveCallbacks -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.removeHandlerCallbacks(intent.kind, invocation) },
            )
            HookIntent.HandlerRemoveCallbacksAndMessages -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.removeHandlerCallbacksAndMessages(invocation) },
            )
            HookIntent.HandlerHasCallbacks -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { emitter, invocation -> emitter.hasHandlerCallbacks(invocation) },
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
    private val emitOriginal: (MethodInvocation) -> Unit,
    private val emitTryCatchBlock: (org.objectweb.asm.Label, org.objectweb.asm.Label, org.objectweb.asm.Label, String?) -> Unit,
) {
    fun wrapOkHttpEventListenerFactory() {
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            OKHTTP_HELPERS,
            "wrapEventListenerFactory",
            "(Lokhttp3/EventListener\$Factory;)Lokhttp3/EventListener\$Factory;",
            false,
        )
    }

    fun installOkHttpEventListenerFactory() {
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            OKHTTP_HELPERS,
            "installEventListenerFactory",
            "(Lokhttp3/OkHttpClient\$Builder;)Lokhttp3/OkHttpClient\$Builder;",
            false,
        )
    }

    fun wrapWebSocketListener() {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            OKHTTP_HELPERS,
            "wrapWebSocketListener",
            "(Lokhttp3/WebSocketListener;Ljava/lang/String;)Lokhttp3/WebSocketListener;",
            false,
        )
    }

    fun postHandlerRunnable(kind: HandlerRunnableKind, invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        val originalRunnable = saved.argumentLocals[0]
        val wrappedRunnable = visitor.newLocal(RUNNABLE_TYPE)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(originalRunnable)
        loadHandlerToken(saved, kind)
        visitor.visitLdcInsn(ownerLabel())
        invokeHook(
            "wrapHandlerRunnable",
            "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/Object;Ljava/lang/String;)Ljava/lang/Runnable;",
        )
        visitor.storeLocal(wrappedRunnable)

        val resultLocal = visitor.newLocal(Type.BOOLEAN_TYPE)
        val throwableLocal = visitor.newLocal(THROWABLE_TYPE)
        val tryStart = org.objectweb.asm.Label()
        val tryEnd = org.objectweb.asm.Label()
        val catchHandler = org.objectweb.asm.Label()
        val done = org.objectweb.asm.Label()
        emitTryCatchBlock(tryStart, tryEnd, catchHandler, null)
        visitor.visitLabel(tryStart)
        loadInvocation(saved, replacementArgument = 0, replacementLocal = wrappedRunnable)
        emitOriginal(invocation)
        visitor.storeLocal(resultLocal)
        visitor.visitLabel(tryEnd)
        emitHandlerPostResult(originalRunnable, wrappedRunnable, resultLocal)
        visitor.loadLocal(resultLocal)
        visitor.goTo(done)
        visitor.visitLabel(catchHandler)
        visitor.storeLocal(throwableLocal)
        emitHandlerPostResult(originalRunnable, wrappedRunnable, null)
        visitor.loadLocal(throwableLocal)
        visitor.throwException()
        visitor.visitLabel(done)
    }

    fun removeHandlerCallbacks(kind: HandlerRemoveCallbacksKind, invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        val originalRunnable = saved.argumentLocals[0]
        val tokenLocal = when (kind) {
            HandlerRemoveCallbacksKind.RUNNABLE -> null
            HandlerRemoveCallbacksKind.RUNNABLE_OBJECT -> saved.argumentLocals[1]
        }

        // Preserve the application's call and exception exactly; JH lookup starts only after it succeeds.
        loadInvocation(saved)
        emitOriginal(invocation)

        val wrappersLocal = visitor.newLocal(RUNNABLE_ARRAY_TYPE)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(originalRunnable)
        loadNullableLocal(tokenLocal)
        invokeHook(
            "handlerWrappers",
            "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/Object;)[Ljava/lang/Runnable;",
        )
        visitor.storeLocal(wrappersLocal)
        emitForEachHandlerWrapper(saved, invocation, wrappersLocal)

        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(originalRunnable)
        loadNullableLocal(tokenLocal)
        invokeHook(
            "clearHandlerWrappers",
            "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/Object;)V",
        )
    }

    fun removeHandlerCallbacksAndMessages(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(saved.argumentLocals[0])
        invokeHook("clearHandlerWrappers", "(Landroid/os/Handler;Ljava/lang/Object;)V")
    }

    fun hasHandlerCallbacks(invocation: MethodInvocation) {
        val saved = saveInstanceInvocation(invocation)
        val originalRunnable = saved.argumentLocals[0]
        val resultLocal = visitor.newLocal(Type.BOOLEAN_TYPE)
        loadInvocation(saved)
        emitOriginal(invocation)
        visitor.storeLocal(resultLocal)

        val done = org.objectweb.asm.Label()
        visitor.loadLocal(resultLocal)
        visitor.ifZCmp(GeneratorAdapter.NE, done)

        val wrappersLocal = visitor.newLocal(RUNNABLE_ARRAY_TYPE)
        visitor.loadLocal(saved.receiverLocal)
        visitor.loadLocal(originalRunnable)
        visitor.visitInsn(Opcodes.ACONST_NULL)
        invokeHook(
            "handlerWrappers",
            "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/Object;)[Ljava/lang/Runnable;",
        )
        visitor.storeLocal(wrappersLocal)
        emitAnyHandlerWrapper(saved, invocation, wrappersLocal, resultLocal, done)
        visitor.visitLabel(done)
        visitor.loadLocal(resultLocal)
    }

    private fun emitForEachHandlerWrapper(
        saved: SavedInvocation,
        invocation: MethodInvocation,
        wrappersLocal: Int,
    ) {
        val indexLocal = visitor.newLocal(Type.INT_TYPE)
        val wrapperLocal = visitor.newLocal(RUNNABLE_TYPE)
        val loop = org.objectweb.asm.Label()
        val next = org.objectweb.asm.Label()
        val finished = org.objectweb.asm.Label()
        visitor.loadLocal(wrappersLocal)
        visitor.ifNull(finished)
        visitor.push(0)
        visitor.storeLocal(indexLocal)
        visitor.visitLabel(loop)
        visitor.loadLocal(indexLocal)
        visitor.loadLocal(wrappersLocal)
        visitor.arrayLength()
        visitor.ifICmp(GeneratorAdapter.GE, finished)
        visitor.loadLocal(wrappersLocal)
        visitor.loadLocal(indexLocal)
        visitor.arrayLoad(RUNNABLE_TYPE)
        visitor.storeLocal(wrapperLocal)

        val tryStart = org.objectweb.asm.Label()
        val tryEnd = org.objectweb.asm.Label()
        val catchHandler = org.objectweb.asm.Label()
        emitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable")
        visitor.visitLabel(tryStart)
        loadInvocation(saved, replacementArgument = 0, replacementLocal = wrapperLocal)
        emitOriginal(invocation)
        visitor.visitLabel(tryEnd)
        visitor.goTo(next)
        visitor.visitLabel(catchHandler)
        visitor.pop()
        visitor.visitLabel(next)
        visitor.iinc(indexLocal, 1)
        visitor.goTo(loop)
        visitor.visitLabel(finished)
    }

    private fun emitAnyHandlerWrapper(
        saved: SavedInvocation,
        invocation: MethodInvocation,
        wrappersLocal: Int,
        resultLocal: Int,
        success: org.objectweb.asm.Label,
    ) {
        val indexLocal = visitor.newLocal(Type.INT_TYPE)
        val wrapperLocal = visitor.newLocal(RUNNABLE_TYPE)
        val loop = org.objectweb.asm.Label()
        val next = org.objectweb.asm.Label()
        val finished = org.objectweb.asm.Label()
        visitor.loadLocal(wrappersLocal)
        visitor.ifNull(finished)
        visitor.push(0)
        visitor.storeLocal(indexLocal)
        visitor.visitLabel(loop)
        visitor.loadLocal(indexLocal)
        visitor.loadLocal(wrappersLocal)
        visitor.arrayLength()
        visitor.ifICmp(GeneratorAdapter.GE, finished)
        visitor.loadLocal(wrappersLocal)
        visitor.loadLocal(indexLocal)
        visitor.arrayLoad(RUNNABLE_TYPE)
        visitor.storeLocal(wrapperLocal)

        val tryStart = org.objectweb.asm.Label()
        val tryEnd = org.objectweb.asm.Label()
        val catchHandler = org.objectweb.asm.Label()
        emitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable")
        visitor.visitLabel(tryStart)
        loadInvocation(saved, replacementArgument = 0, replacementLocal = wrapperLocal)
        emitOriginal(invocation)
        visitor.visitLabel(tryEnd)
        visitor.ifZCmp(GeneratorAdapter.EQ, next)
        visitor.push(true)
        visitor.storeLocal(resultLocal)
        visitor.goTo(success)
        visitor.visitLabel(catchHandler)
        visitor.pop()
        visitor.visitLabel(next)
        visitor.iinc(indexLocal, 1)
        visitor.goTo(loop)
        visitor.visitLabel(finished)
    }

    private fun emitHandlerPostResult(originalLocal: Int, wrappedLocal: Int, resultLocal: Int?) {
        visitor.loadLocal(originalLocal)
        visitor.loadLocal(wrappedLocal)
        if (resultLocal == null) visitor.push(false) else visitor.loadLocal(resultLocal)
        invokeHook("onHandlerPostResult", "(Ljava/lang/Runnable;Ljava/lang/Runnable;Z)V")
    }

    private fun loadHandlerToken(saved: SavedInvocation, kind: HandlerRunnableKind) {
        when (kind) {
            HandlerRunnableKind.RUNNABLE_OBJECT_LONG_DELAY,
            HandlerRunnableKind.RUNNABLE_OBJECT_LONG_TIME -> visitor.loadLocal(saved.argumentLocals[1])
            else -> visitor.visitInsn(Opcodes.ACONST_NULL)
        }
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

    private fun loadInvocation(
        saved: SavedInvocation,
        replacementArgument: Int = -1,
        replacementLocal: Int = -1,
    ) {
        visitor.loadLocal(saved.receiverLocal)
        saved.argumentTypes.indices.forEach { index ->
            if (index == replacementArgument) {
                visitor.loadLocal(replacementLocal)
            } else {
                visitor.loadLocal(saved.argumentLocals[index], saved.argumentTypes[index])
            }
        }
    }

    private fun loadNullableLocal(local: Int?) {
        if (local == null) visitor.visitInsn(Opcodes.ACONST_NULL) else visitor.loadLocal(local)
    }

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
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "wrapClickListener",
            "(Landroid/view/View\$OnClickListener;Ljava/lang/String;)Landroid/view/View\$OnClickListener;",
            false,
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

    private fun wrapTopRunnable() {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "wrapRunnable",
            "(Ljava/lang/Runnable;Ljava/lang/String;)Ljava/lang/Runnable;",
            false,
        )
    }

    private fun wrapTopCallable() {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "wrapCallable",
            "(Ljava/util/concurrent/Callable;Ljava/lang/String;)Ljava/util/concurrent/Callable;",
            false,
        )
    }

    private fun wrapTopCoroutineBlock() {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "wrapCoroutineBlock",
            "(Lkotlin/jvm/functions/Function2;Ljava/lang/String;)Lkotlin/jvm/functions/Function2;",
            false,
        )
    }

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
        private val OBJECT_TYPE: Type = Type.getType("Ljava/lang/Object;")
        private val RUNNABLE_TYPE: Type = Type.getType("Ljava/lang/Runnable;")
        private val RUNNABLE_ARRAY_TYPE: Type = Type.getType("[Ljava/lang/Runnable;")
        private val THROWABLE_TYPE: Type = Type.getType("Ljava/lang/Throwable;")
    }
}
