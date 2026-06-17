package io.jankhunter.gradle

import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter

internal interface BytecodeCommand {
    val id: String
    val replacesOriginalCall: Boolean
    fun emit(emitter: HookBytecodeEmitter)
}

internal object BytecodeCommandFactory {
    fun commandFor(intent: HookIntent): BytecodeCommand {
        return when (intent) {
            HookIntent.WrapOkHttpEventListenerFactory -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = HookBytecodeEmitter::wrapOkHttpEventListenerFactory,
            )
            HookIntent.InstallOkHttpEventListenerFactory -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = HookBytecodeEmitter::installOkHttpEventListenerFactory,
            )
            HookIntent.WrapWebSocketListener -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = HookBytecodeEmitter::wrapWebSocketListener,
            )
            is HookIntent.HandlerRunnable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { it.postHandlerRunnable(intent.kind) },
            )
            is HookIntent.HandlerRemoveCallbacks -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = { it.removeHandlerCallbacks(intent.kind) },
            )
            HookIntent.HandlerRemoveCallbacksAndMessages -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = HookBytecodeEmitter::removeHandlerCallbacksAndMessages,
            )
            HookIntent.HandlerHasCallbacks -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = true,
                action = HookBytecodeEmitter::hasHandlerCallbacks,
            )
            HookIntent.HandlerMessageSend -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { it.recordCallCounter("handler.send_message") },
            )
            is HookIntent.ExecutorRunnable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { it.wrapExecutorRunnable(intent.kind) },
            )
            is HookIntent.ExecutorCallable -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { it.wrapExecutorCallable(intent.kind) },
            )
            is HookIntent.CoroutineBlock -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { it.wrapCoroutineBlock(intent.kind) },
            )
            HookIntent.WrapClickListener -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = HookBytecodeEmitter::wrapTopClickListener,
            )
            is HookIntent.LogSpam -> SimpleCommand(
                id = intent.id,
                replacesOriginalCall = false,
                action = { it.recordLogSpam(intent.source, intent.level) },
            )
        }
    }
}

private data class SimpleCommand(
    override val id: String,
    override val replacesOriginalCall: Boolean,
    private val action: (HookBytecodeEmitter) -> Unit,
) : BytecodeCommand {
    override fun emit(emitter: HookBytecodeEmitter) {
        action(emitter)
    }
}

internal class HookBytecodeEmitter(
    private val visitor: AdviceAdapter,
    private val ownerLabel: () -> String,
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

    fun postHandlerRunnable(kind: HandlerRunnableKind) {
        visitor.visitLdcInsn(ownerLabel())
        val methodName = when (kind) {
            HandlerRunnableKind.SINGLE_RUNNABLE -> "postHandlerRunnable"
            HandlerRunnableKind.FRONT_RUNNABLE -> "postHandlerRunnableAtFront"
            HandlerRunnableKind.RUNNABLE_LONG_DELAY -> "postHandlerRunnableDelayed"
            HandlerRunnableKind.RUNNABLE_LONG_TIME -> "postHandlerRunnableAtTime"
            HandlerRunnableKind.RUNNABLE_OBJECT_LONG_DELAY -> "postHandlerRunnableDelayed"
            HandlerRunnableKind.RUNNABLE_OBJECT_LONG_TIME -> "postHandlerRunnableAtTime"
        }
        val methodDescriptor = when (kind) {
            HandlerRunnableKind.SINGLE_RUNNABLE,
            HandlerRunnableKind.FRONT_RUNNABLE -> {
                "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/String;)Z"
            }
            HandlerRunnableKind.RUNNABLE_LONG_DELAY,
            HandlerRunnableKind.RUNNABLE_LONG_TIME -> {
                "(Landroid/os/Handler;Ljava/lang/Runnable;JLjava/lang/String;)Z"
            }
            HandlerRunnableKind.RUNNABLE_OBJECT_LONG_DELAY,
            HandlerRunnableKind.RUNNABLE_OBJECT_LONG_TIME -> {
                "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/Object;JLjava/lang/String;)Z"
            }
        }
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            methodName,
            methodDescriptor,
            false,
        )
    }

    fun removeHandlerCallbacks(kind: HandlerRemoveCallbacksKind) {
        val methodDescriptor = when (kind) {
            HandlerRemoveCallbacksKind.RUNNABLE -> "(Landroid/os/Handler;Ljava/lang/Runnable;)V"
            HandlerRemoveCallbacksKind.RUNNABLE_OBJECT -> {
                "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/Object;)V"
            }
        }
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "removeHandlerCallbacks",
            methodDescriptor,
            false,
        )
    }

    fun removeHandlerCallbacksAndMessages() {
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "removeHandlerCallbacksAndMessages",
            "(Landroid/os/Handler;Ljava/lang/Object;)V",
            false,
        )
    }

    fun hasHandlerCallbacks() {
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "hasHandlerCallbacks",
            "(Landroid/os/Handler;Ljava/lang/Runnable;)Z",
            false,
        )
    }

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
            JANK_HUNTER,
            "wrapClickListener",
            "(Landroid/view/View\$OnClickListener;Ljava/lang/String;)Landroid/view/View\$OnClickListener;",
            false,
        )
    }

    fun recordCallCounter(prefix: String) {
        visitor.visitLdcInsn("owner.${ownerLabel()}.$prefix.count")
        visitor.visitInsn(Opcodes.LCONST_1)
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
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
            JANK_HUNTER,
            "recordLogSpam",
            "(Ljava/lang/String;Ljava/lang/String;I)V",
            false,
        )
    }

    private fun wrapTopRunnable() {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "wrapRunnable",
            "(Ljava/lang/Runnable;Ljava/lang/String;)Ljava/lang/Runnable;",
            false,
        )
    }

    private fun wrapTopCallable() {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "wrapCallable",
            "(Ljava/util/concurrent/Callable;Ljava/lang/String;)Ljava/util/concurrent/Callable;",
            false,
        )
    }

    private fun wrapTopCoroutineBlock() {
        visitor.visitLdcInsn(ownerLabel())
        visitor.visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
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
        private const val JANK_HUNTER = "io/jankhunter/runtime/JankHunter"
        private const val OKHTTP_HELPERS = "io/jankhunter/okhttp3/JankHunterOkHttp3"
        private val OBJECT_TYPE: Type = Type.getType("Ljava/lang/Object;")
    }
}
