package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val params = parameters.get()
        return JankHunterClassVisitor(
            nextClassVisitor,
            classContext.currentClassData.className,
            HookConfig(
                methodCounters = params.methodCounters.getOrElse(false),
                okhttp = params.okhttp.getOrElse(false),
                webSockets = params.webSockets.getOrElse(false),
                handlers = params.handlers.getOrElse(false),
                executors = params.executors.getOrElse(false),
            ),
        )
    }

    override fun isInstrumentable(classData: ClassData): Boolean {
        val params = parameters.get()
        val hooksEnabled = params.methodCounters.getOrElse(false) ||
            params.okhttp.getOrElse(false) ||
            params.webSockets.getOrElse(false) ||
            params.handlers.getOrElse(false) ||
            params.executors.getOrElse(false)
        if (!hooksEnabled) return false
        return InstrumentationMatcher(
            params.includePackages.getOrElse(emptyList()),
            params.excludePackages.getOrElse(emptyList()),
        ).matches(classData.className)
    }
}

private data class HookConfig(
    val methodCounters: Boolean,
    val okhttp: Boolean,
    val webSockets: Boolean,
    val handlers: Boolean,
    val executors: Boolean,
)

private class JankHunterClassVisitor(
    next: ClassVisitor,
    private val className: String,
    private val config: HookConfig,
) : ClassVisitor(Opcodes.ASM9, next) {
    override fun visitMethod(
        access: Int,
        name: String,
        descriptor: String,
        signature: String?,
        exceptions: Array<out String>?,
    ): MethodVisitor {
        val next = super.visitMethod(access, name, descriptor, signature, exceptions)
        if (name == "<init>" || name == "<clinit>") return next
        if (access and Opcodes.ACC_ABSTRACT != 0) return next
        if (access and Opcodes.ACC_NATIVE != 0) return next
        return JankHunterMethodVisitor(next, access, name, descriptor, className, config)
    }
}

private class JankHunterMethodVisitor(
    next: MethodVisitor,
    access: Int,
    private val methodName: String,
    private val methodDescriptor: String,
    private val className: String,
    private val config: HookConfig,
) : AdviceAdapter(Opcodes.ASM9, next, access, methodName, methodDescriptor) {
    private val ownerLabel = OwnerIds.ownerLabel(className, methodName, methodDescriptor)

    override fun onMethodEnter() {
        if (config.methodCounters) {
            visitLdcInsn("owner.$ownerLabel")
            visitInsn(Opcodes.LCONST_1)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER,
                "recordCounter",
                "(Ljava/lang/String;J)V",
                false,
            )
        }
    }

    override fun visitMethodInsn(
        opcodeAndSource: Int,
        owner: String,
        name: String,
        descriptor: String,
        isInterface: Boolean,
    ) {
        when {
            config.okhttp && InstrumentationHooks.isOkHttpEventListenerFactory(owner, name, descriptor) -> {
                wrapOkHttpEventListenerFactory()
            }

            config.webSockets && InstrumentationHooks.isOkHttpNewWebSocket(owner, name, descriptor) -> {
                wrapWebSocketListener()
            }

            config.handlers && InstrumentationHooks.handlerRunnableKind(owner, name, descriptor) == HandlerRunnableKind.SINGLE_RUNNABLE -> {
                wrapTopRunnable()
            }

            config.handlers && InstrumentationHooks.handlerRunnableKind(owner, name, descriptor) == HandlerRunnableKind.RUNNABLE_LONG -> {
                wrapRunnableBeforeLong()
            }

            config.handlers && InstrumentationHooks.handlerRunnableKind(owner, name, descriptor) == HandlerRunnableKind.RUNNABLE_OBJECT_LONG -> {
                wrapRunnableBeforeObjectAndLong()
            }

            config.handlers && InstrumentationHooks.isHandlerMessageSend(owner, name, descriptor) -> {
                recordCallCounter("handler.send_message")
            }

            config.executors && InstrumentationHooks.executorRunnableKind(owner, name, descriptor) == ExecutorRunnableKind.SINGLE_RUNNABLE -> {
                wrapTopRunnable()
            }

            config.executors && InstrumentationHooks.executorRunnableKind(owner, name, descriptor) == ExecutorRunnableKind.RUNNABLE_OBJECT -> {
                wrapRunnableBeforeObject()
            }

            config.executors && InstrumentationHooks.executorRunnableKind(owner, name, descriptor) == ExecutorRunnableKind.RUNNABLE_LONG_OBJECT -> {
                wrapRunnableBeforeLongAndObject()
            }

            config.executors && InstrumentationHooks.executorRunnableKind(owner, name, descriptor) == ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT -> {
                wrapRunnableBeforeTwoLongsAndObject()
            }

            config.executors && InstrumentationHooks.executorCallableKind(owner, name, descriptor) == ExecutorCallableKind.SINGLE_CALLABLE -> {
                wrapTopCallable()
            }

            config.executors && InstrumentationHooks.executorCallableKind(owner, name, descriptor) == ExecutorCallableKind.CALLABLE_LONG_OBJECT -> {
                wrapCallableBeforeLongAndObject()
            }
        }
        super.visitMethodInsn(opcodeAndSource, owner, name, descriptor, isInterface)
    }

    override fun visitMaxs(maxStack: Int, maxLocals: Int) {
        super.visitMaxs(maxStack + 6, maxLocals)
    }

    private fun wrapOkHttpEventListenerFactory() {
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            OKHTTP_HELPERS,
            "wrapEventListenerFactory",
            "(Lokhttp3/EventListener\$Factory;)Lokhttp3/EventListener\$Factory;",
            false,
        )
    }

    private fun wrapWebSocketListener() {
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            OKHTTP_HELPERS,
            "wrapWebSocketListener",
            "(Lokhttp3/WebSocketListener;Ljava/lang/String;)Lokhttp3/WebSocketListener;",
            false,
        )
    }

    private fun wrapTopRunnable() {
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "wrapRunnable",
            "(Ljava/lang/Runnable;Ljava/lang/String;)Ljava/lang/Runnable;",
            false,
        )
    }

    private fun wrapTopCallable() {
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "wrapCallable",
            "(Ljava/util/concurrent/Callable;Ljava/lang/String;)Ljava/util/concurrent/Callable;",
            false,
        )
    }

    private fun wrapRunnableBeforeLong() {
        val delayLocal = newLocal(Type.LONG_TYPE)
        storeLocal(delayLocal)
        wrapTopRunnable()
        loadLocal(delayLocal)
    }

    private fun wrapRunnableBeforeObject() {
        val objectLocal = newLocal(OBJECT_TYPE)
        storeLocal(objectLocal)
        wrapTopRunnable()
        loadLocal(objectLocal)
    }

    private fun wrapRunnableBeforeObjectAndLong() {
        val delayLocal = newLocal(Type.LONG_TYPE)
        val objectLocal = newLocal(OBJECT_TYPE)
        storeLocal(delayLocal)
        storeLocal(objectLocal)
        wrapTopRunnable()
        loadLocal(objectLocal)
        loadLocal(delayLocal)
    }

    private fun wrapRunnableBeforeLongAndObject() {
        val objectLocal = newLocal(OBJECT_TYPE)
        val delayLocal = newLocal(Type.LONG_TYPE)
        storeLocal(objectLocal)
        storeLocal(delayLocal)
        wrapTopRunnable()
        loadLocal(delayLocal)
        loadLocal(objectLocal)
    }

    private fun wrapRunnableBeforeTwoLongsAndObject() {
        val objectLocal = newLocal(OBJECT_TYPE)
        val secondLongLocal = newLocal(Type.LONG_TYPE)
        val firstLongLocal = newLocal(Type.LONG_TYPE)
        storeLocal(objectLocal)
        storeLocal(secondLongLocal)
        storeLocal(firstLongLocal)
        wrapTopRunnable()
        loadLocal(firstLongLocal)
        loadLocal(secondLongLocal)
        loadLocal(objectLocal)
    }

    private fun wrapCallableBeforeLongAndObject() {
        val objectLocal = newLocal(OBJECT_TYPE)
        val delayLocal = newLocal(Type.LONG_TYPE)
        storeLocal(objectLocal)
        storeLocal(delayLocal)
        wrapTopCallable()
        loadLocal(delayLocal)
        loadLocal(objectLocal)
    }

    private fun recordCallCounter(prefix: String) {
        visitLdcInsn("owner.$ownerLabel.$prefix.count")
        visitInsn(Opcodes.LCONST_1)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "recordCounter",
            "(Ljava/lang/String;J)V",
            false,
        )
    }

    companion object {
        private const val JANK_HUNTER = "io/jankhunter/runtime/JankHunter"
        private const val OKHTTP_HELPERS = "io/jankhunter/okhttp3/JankHunterOkHttp3"
        private val OBJECT_TYPE: Type = Type.getType("Ljava/lang/Object;")
    }
}

internal object InstrumentationHooks {
    fun isOkHttpEventListenerFactory(owner: String, name: String, descriptor: String): Boolean {
        return owner == "okhttp3/OkHttpClient\$Builder" &&
            name == "eventListenerFactory" &&
            descriptor == "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;"
    }

    fun isOkHttpNewWebSocket(owner: String, name: String, descriptor: String): Boolean {
        return owner == "okhttp3/OkHttpClient" &&
            name == "newWebSocket" &&
            descriptor == "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;"
    }

    fun handlerRunnableKind(owner: String, name: String, descriptor: String): HandlerRunnableKind? {
        if (owner != "android/os/Handler") return null
        return when {
            name == "post" && descriptor == "(Ljava/lang/Runnable;)Z" -> HandlerRunnableKind.SINGLE_RUNNABLE
            name == "postAtFrontOfQueue" && descriptor == "(Ljava/lang/Runnable;)Z" -> HandlerRunnableKind.SINGLE_RUNNABLE
            name == "postDelayed" && descriptor == "(Ljava/lang/Runnable;J)Z" -> HandlerRunnableKind.RUNNABLE_LONG
            name == "postAtTime" && descriptor == "(Ljava/lang/Runnable;J)Z" -> HandlerRunnableKind.RUNNABLE_LONG
            name == "postDelayed" && descriptor == "(Ljava/lang/Runnable;Ljava/lang/Object;J)Z" -> HandlerRunnableKind.RUNNABLE_OBJECT_LONG
            name == "postAtTime" && descriptor == "(Ljava/lang/Runnable;Ljava/lang/Object;J)Z" -> HandlerRunnableKind.RUNNABLE_OBJECT_LONG
            else -> null
        }
    }

    fun isHandlerMessageSend(owner: String, name: String, descriptor: String): Boolean {
        if (owner != "android/os/Handler") return false
        return when (name) {
            "sendMessage",
            "sendMessageAtFrontOfQueue" -> descriptor == "(Landroid/os/Message;)Z"
            "sendMessageDelayed",
            "sendMessageAtTime" -> descriptor == "(Landroid/os/Message;J)Z"
            else -> false
        }
    }

    fun executorRunnableKind(owner: String, name: String, descriptor: String): ExecutorRunnableKind? {
        if (!isExecutorOwner(owner)) return null
        return when {
            name == "execute" && descriptor == "(Ljava/lang/Runnable;)V" -> ExecutorRunnableKind.SINGLE_RUNNABLE
            name == "submit" && descriptor == "(Ljava/lang/Runnable;)Ljava/util/concurrent/Future;" -> ExecutorRunnableKind.SINGLE_RUNNABLE
            name == "submit" && descriptor == "(Ljava/lang/Runnable;Ljava/lang/Object;)Ljava/util/concurrent/Future;" -> ExecutorRunnableKind.RUNNABLE_OBJECT
            name == "schedule" && descriptor == "(Ljava/lang/Runnable;JLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;" -> ExecutorRunnableKind.RUNNABLE_LONG_OBJECT
            name == "scheduleAtFixedRate" && descriptor == "(Ljava/lang/Runnable;JJLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;" -> ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT
            name == "scheduleWithFixedDelay" && descriptor == "(Ljava/lang/Runnable;JJLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;" -> ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT
            else -> null
        }
    }

    fun executorCallableKind(owner: String, name: String, descriptor: String): ExecutorCallableKind? {
        if (!isExecutorOwner(owner)) return null
        return when {
            name == "submit" && descriptor == "(Ljava/util/concurrent/Callable;)Ljava/util/concurrent/Future;" -> ExecutorCallableKind.SINGLE_CALLABLE
            name == "schedule" && descriptor == "(Ljava/util/concurrent/Callable;JLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;" -> ExecutorCallableKind.CALLABLE_LONG_OBJECT
            else -> null
        }
    }

    private fun isExecutorOwner(owner: String): Boolean {
        return owner in executorOwners
    }

    private val executorOwners = setOf(
        "java/util/concurrent/Executor",
        "java/util/concurrent/ExecutorService",
        "java/util/concurrent/ScheduledExecutorService",
        "java/util/concurrent/AbstractExecutorService",
        "java/util/concurrent/ThreadPoolExecutor",
        "java/util/concurrent/ScheduledThreadPoolExecutor",
        "java/util/concurrent/ForkJoinPool",
    )
}

internal enum class HandlerRunnableKind {
    SINGLE_RUNNABLE,
    RUNNABLE_LONG,
    RUNNABLE_OBJECT_LONG,
}

internal enum class ExecutorRunnableKind {
    SINGLE_RUNNABLE,
    RUNNABLE_OBJECT,
    RUNNABLE_LONG_OBJECT,
    RUNNABLE_LONG_LONG_OBJECT,
}

internal enum class ExecutorCallableKind {
    SINGLE_CALLABLE,
    CALLABLE_LONG_OBJECT,
}

internal object OwnerIds {
    fun ownerLabel(className: String, methodName: String, descriptor: String): String {
        val normalizedClass = className.replace('/', '.')
        val readable = "$normalizedClass.$methodName"
        val id = fnv1a64("$readable$descriptor")
        return "$readable#${id.toString(16)}"
    }

    private fun fnv1a64(value: String): ULong {
        var hash = 0xcbf29ce484222325UL
        for (byte in value.encodeToByteArray()) {
            hash = hash xor byte.toUByte().toULong()
            hash *= 0x100000001b3UL
        }
        return hash
    }
}
