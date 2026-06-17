package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter
import java.io.File

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val params = parameters.get()
        val hookConfig = HookConfig(
            methodCounters = params.methodCounters.getOrElse(false),
            okhttp = params.okhttp.getOrElse(false),
            webSockets = params.webSockets.getOrElse(false),
            handlers = params.handlers.getOrElse(false),
            executors = params.executors.getOrElse(false),
            coroutines = params.coroutines.getOrElse(false),
            flowInteractions = params.flowInteractions.getOrElse(false),
            logSpam = params.logSpam.getOrElse(false),
            classGraph = params.classGraph.getOrElse(false),
            runtimeCallGraph = params.runtimeCallGraph.getOrElse(false),
            classGraphPath = params.classGraphPath.getOrElse(""),
        )
        if (params.asmProgressLog.getOrElse(false)) {
            AsmProgressReporter.recordInstrumented(
                params.progressLabel.getOrElse("unknown"),
                classContext.currentClassData.className,
                hookConfig.progressLabel(),
            )
        }
        return JankHunterClassVisitor(
            nextClassVisitor,
            classContext.currentClassData.className,
            hookConfig,
        )
    }

    override fun isInstrumentable(classData: ClassData): Boolean {
        val params = parameters.get()
        val hooksEnabled = params.methodCounters.getOrElse(false) ||
            params.okhttp.getOrElse(false) ||
            params.webSockets.getOrElse(false) ||
            params.handlers.getOrElse(false) ||
            params.executors.getOrElse(false) ||
            params.coroutines.getOrElse(false) ||
            params.flowInteractions.getOrElse(false) ||
            params.logSpam.getOrElse(false) ||
            params.classGraph.getOrElse(false) ||
            params.runtimeCallGraph.getOrElse(false)
        val matched = hooksEnabled && InstrumentationMatcher(
            params.includePackages.getOrElse(emptyList()),
            params.excludePackages.getOrElse(emptyList()),
            params.allowEmptyIncludePackages.getOrElse(false),
        ).matches(classData.className)
        if (params.asmProgressLog.getOrElse(false)) {
            AsmProgressReporter.recordScanned(
                params.progressLabel.getOrElse("unknown"),
                classData.className,
                matched,
            )
        }
        return matched
    }
}

internal data class HookConfig(
    val methodCounters: Boolean,
    val okhttp: Boolean,
    val webSockets: Boolean,
    val handlers: Boolean,
    val executors: Boolean,
    val coroutines: Boolean,
    val flowInteractions: Boolean,
    val logSpam: Boolean,
    val classGraph: Boolean,
    val runtimeCallGraph: Boolean,
    val classGraphPath: String,
) {
    fun progressLabel(): String {
        return buildList {
            if (methodCounters) add("methods")
            if (okhttp) add("okhttp")
            if (webSockets) add("websocket")
            if (handlers) add("handler")
            if (executors) add("executor")
            if (coroutines) add("coroutine")
            if (flowInteractions) add("flow")
            if (logSpam) add("logspam")
            if (classGraph) add("graph")
            if (runtimeCallGraph) add("runtimegraph")
        }.joinToString("+").ifEmpty { "none" }
    }
}

internal class JankHunterClassVisitor(
    next: ClassVisitor,
    private val className: String,
    private val config: HookConfig,
) : ClassVisitor(Opcodes.ASM9, next) {
    private val edges = linkedMapOf<ClassGraphEdgeKey, Int>()

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
        return JankHunterMethodVisitor(next, access, name, descriptor, className, config) { calleeOwner, calleeName ->
            recordStaticEdge(name, descriptor, calleeOwner, calleeName)
        }
    }

    override fun visitEnd() {
        if (config.classGraph) {
            ClassGraphWriter.append(config.classGraphPath, className, edges)
        }
        super.visitEnd()
    }

    private fun recordStaticEdge(
        callerName: String,
        callerDescriptor: String,
        calleeOwner: String,
        calleeName: String,
    ) {
        if (!config.classGraph) return
        if (!ClassGraphWriter.isApplicationLike(calleeOwner)) return
        val key = ClassGraphEdgeKey(
            caller = "$callerName$callerDescriptor",
            calleeClass = calleeOwner.replace('/', '.'),
            calleeMethod = calleeName,
        )
        edges[key] = (edges[key] ?: 0) + 1
    }
}

private class JankHunterMethodVisitor(
    next: MethodVisitor,
    access: Int,
    private val methodName: String,
    private val methodDescriptor: String,
    private val className: String,
    private val config: HookConfig,
    private val recordStaticEdge: (String, String) -> Unit,
) : AdviceAdapter(Opcodes.ASM9, next, access, methodName, methodDescriptor) {
    private val ownerLabel = OwnerIds.ownerLabel(className, methodName, methodDescriptor)
    private var runtimeCallStartLocal = -1
    private val runtimeCallTryStart = Label()
    private val runtimeCallTryEnd = Label()
    private val runtimeCallExceptionHandler = Label()

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
        if (config.runtimeCallGraph) {
            visitLdcInsn(ownerLabel)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER,
                "enterMethod",
                "(Ljava/lang/String;)J",
                false,
            )
            runtimeCallStartLocal = newLocal(Type.LONG_TYPE)
            storeLocal(runtimeCallStartLocal)
            visitLabel(runtimeCallTryStart)
        }
    }

    override fun onMethodExit(opcode: Int) {
        if (config.runtimeCallGraph && runtimeCallStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitRuntimeCallExit()
        }
    }

    private fun emitRuntimeCallExit() {
        loadLocal(runtimeCallStartLocal)
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "exitMethod",
            "(JLjava/lang/String;)V",
            false,
        )
    }

    override fun visitMethodInsn(
        opcodeAndSource: Int,
        owner: String,
        name: String,
        descriptor: String,
        isInterface: Boolean,
    ) {
        recordStaticEdge(owner, name)
        val handlerRunnableKind = if (config.handlers) {
            InstrumentationHooks.handlerRunnableKind(owner, name, descriptor)
        } else {
            null
        }
        val handlerRemoveCallbacksKind = if (config.handlers) {
            InstrumentationHooks.handlerRemoveCallbacksKind(owner, name, descriptor)
        } else {
            null
        }
        val executorRunnableKind = if (config.executors) {
            InstrumentationHooks.executorRunnableKind(owner, name, descriptor)
        } else {
            null
        }
        val executorCallableKind = if (config.executors) {
            InstrumentationHooks.executorCallableKind(owner, name, descriptor)
        } else {
            null
        }
        val coroutineBlockKind = if (config.coroutines) {
            InstrumentationHooks.coroutineBlockKind(owner, name, descriptor)
        } else {
            null
        }
        when {
            config.okhttp && InstrumentationHooks.isOkHttpEventListenerFactory(owner, name, descriptor) -> {
                wrapOkHttpEventListenerFactory()
            }

            config.okhttp && InstrumentationHooks.isOkHttpBuild(owner, name, descriptor) -> {
                installOkHttpEventListenerFactory()
            }

            config.webSockets && InstrumentationHooks.isOkHttpNewWebSocket(owner, name, descriptor) -> {
                wrapWebSocketListener()
            }

            handlerRunnableKind != null -> {
                postHandlerRunnable(handlerRunnableKind)
                return
            }

            handlerRemoveCallbacksKind != null -> {
                removeHandlerCallbacks(handlerRemoveCallbacksKind)
                return
            }

            config.handlers && InstrumentationHooks.isHandlerRemoveCallbacksAndMessages(owner, name, descriptor) -> {
                removeHandlerCallbacksAndMessages()
                return
            }

            config.handlers && InstrumentationHooks.isHandlerHasCallbacks(owner, name, descriptor) -> {
                hasHandlerCallbacks()
                return
            }

            config.handlers && InstrumentationHooks.isHandlerMessageSend(owner, name, descriptor) -> {
                recordCallCounter("handler.send_message")
            }

            executorRunnableKind == ExecutorRunnableKind.SINGLE_RUNNABLE -> {
                wrapTopRunnable()
            }

            executorRunnableKind == ExecutorRunnableKind.RUNNABLE_OBJECT -> {
                wrapRunnableBeforeObject()
            }

            executorRunnableKind == ExecutorRunnableKind.RUNNABLE_LONG_OBJECT -> {
                wrapRunnableBeforeLongAndObject()
            }

            executorRunnableKind == ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT -> {
                wrapRunnableBeforeTwoLongsAndObject()
            }

            executorCallableKind == ExecutorCallableKind.SINGLE_CALLABLE -> {
                wrapTopCallable()
            }

            executorCallableKind == ExecutorCallableKind.CALLABLE_LONG_OBJECT -> {
                wrapCallableBeforeLongAndObject()
            }

            coroutineBlockKind == CoroutineBlockKind.TOP_FUNCTION2 -> {
                wrapTopCoroutineBlock()
            }

            coroutineBlockKind == CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION -> {
                wrapCoroutineBlockBeforeContinuation()
            }

            coroutineBlockKind == CoroutineBlockKind.FUNCTION2_BEFORE_INT_OBJECT -> {
                wrapCoroutineBlockBeforeIntAndObject()
            }

            config.flowInteractions && InstrumentationHooks.isViewSetOnClickListener(owner, name, descriptor) -> {
                wrapTopClickListener()
            }

            config.logSpam && InstrumentationHooks.logSpamLevel(owner, name) != null -> {
                recordLogSpam(owner, name, InstrumentationHooks.logSpamLevel(owner, name) ?: 0)
            }
        }
        super.visitMethodInsn(opcodeAndSource, owner, name, descriptor, isInterface)
    }

    override fun visitMaxs(maxStack: Int, maxLocals: Int) {
        if (config.runtimeCallGraph && runtimeCallStartLocal >= 0) {
            visitLabel(runtimeCallTryEnd)
            visitTryCatchBlock(runtimeCallTryStart, runtimeCallTryEnd, runtimeCallExceptionHandler, null)
            visitLabel(runtimeCallExceptionHandler)
            val throwableLocal = newLocal(Type.getType(Throwable::class.java))
            storeLocal(throwableLocal)
            emitRuntimeCallExit()
            loadLocal(throwableLocal)
            visitInsn(Opcodes.ATHROW)
        }
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

    private fun installOkHttpEventListenerFactory() {
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            OKHTTP_HELPERS,
            "installEventListenerFactory",
            "(Lokhttp3/OkHttpClient\$Builder;)Lokhttp3/OkHttpClient\$Builder;",
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

    private fun postHandlerRunnable(kind: HandlerRunnableKind) {
        visitLdcInsn(ownerLabel)
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
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            methodName,
            methodDescriptor,
            false,
        )
    }

    private fun removeHandlerCallbacks(kind: HandlerRemoveCallbacksKind) {
        val methodDescriptor = when (kind) {
            HandlerRemoveCallbacksKind.RUNNABLE -> "(Landroid/os/Handler;Ljava/lang/Runnable;)V"
            HandlerRemoveCallbacksKind.RUNNABLE_OBJECT -> {
                "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/lang/Object;)V"
            }
        }
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "removeHandlerCallbacks",
            methodDescriptor,
            false,
        )
    }

    private fun removeHandlerCallbacksAndMessages() {
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "removeHandlerCallbacksAndMessages",
            "(Landroid/os/Handler;Ljava/lang/Object;)V",
            false,
        )
    }

    private fun hasHandlerCallbacks() {
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "hasHandlerCallbacks",
            "(Landroid/os/Handler;Ljava/lang/Runnable;)Z",
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

    private fun wrapTopCoroutineBlock() {
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "wrapCoroutineBlock",
            "(Lkotlin/jvm/functions/Function2;Ljava/lang/String;)Lkotlin/jvm/functions/Function2;",
            false,
        )
    }

    private fun wrapTopClickListener() {
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "wrapClickListener",
            "(Landroid/view/View\$OnClickListener;Ljava/lang/String;)Landroid/view/View\$OnClickListener;",
            false,
        )
    }

    private fun wrapRunnableBeforeObject() {
        val objectLocal = newLocal(OBJECT_TYPE)
        storeLocal(objectLocal)
        wrapTopRunnable()
        loadLocal(objectLocal)
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

    private fun wrapCoroutineBlockBeforeContinuation() {
        val continuationLocal = newLocal(OBJECT_TYPE)
        storeLocal(continuationLocal)
        wrapTopCoroutineBlock()
        loadLocal(continuationLocal)
    }

    private fun wrapCoroutineBlockBeforeIntAndObject() {
        val objectLocal = newLocal(OBJECT_TYPE)
        val maskLocal = newLocal(Type.INT_TYPE)
        storeLocal(objectLocal)
        storeLocal(maskLocal)
        wrapTopCoroutineBlock()
        loadLocal(maskLocal)
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

    private fun recordLogSpam(owner: String, name: String, level: Int) {
        visitLdcInsn(ownerLabel)
        visitLdcInsn(InstrumentationHooks.logSpamSource(owner, name))
        push(level)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "recordLogSpam",
            "(Ljava/lang/String;Ljava/lang/String;I)V",
            false,
        )
    }

    companion object {
        private const val JANK_HUNTER = "io/jankhunter/runtime/JankHunter"
        private const val OKHTTP_HELPERS = "io/jankhunter/okhttp3/JankHunterOkHttp3"
        private val OBJECT_TYPE: Type = Type.getType("Ljava/lang/Object;")
    }
}

internal data class ClassGraphEdgeKey(
    val caller: String,
    val calleeClass: String,
    val calleeMethod: String,
)

internal object ClassGraphWriter {
    private val preparedPaths = mutableSetOf<String>()

    @Synchronized
    fun prepare(path: String) {
        if (path.isBlank()) return
        val file = File(path)
        file.parentFile?.mkdirs()
        file.delete()
        preparedPaths.add(file.absolutePath)
    }

    @Synchronized
    fun append(path: String, className: String, edges: Map<ClassGraphEdgeKey, Int>) {
        if (path.isBlank() || edges.isEmpty()) return
        val file = File(path)
        file.parentFile?.mkdirs()
        if (preparedPaths.add(file.absolutePath) && file.exists()) {
            file.delete()
        }
        file.appendText(record(className.replace('/', '.'), edges))
    }

    fun isApplicationLike(owner: String): Boolean {
        val normalized = owner.replace('/', '.')
        return builtinPrefixes.none { normalized.startsWith(it) }
    }

    private fun record(className: String, edges: Map<ClassGraphEdgeKey, Int>): String {
        return buildString {
            append("{\"format\":1,\"class\":\"")
            append(escape(className))
            append("\",\"edges\":[")
            edges.entries.forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append("{\"caller\":\"")
                append(escape(entry.key.caller))
                append("\",\"calleeClass\":\"")
                append(escape(entry.key.calleeClass))
                append("\",\"calleeMethod\":\"")
                append(escape(entry.key.calleeMethod))
                append("\",\"count\":")
                append(entry.value)
                append('}')
            }
            append("]}\n")
        }
    }

    private fun escape(value: String): String {
        return value
            .replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\r", "\\r")
    }

    private val builtinPrefixes = listOf(
        "android.",
        "androidx.",
        "java.",
        "javax.",
        "kotlin.",
        "kotlinx.",
        "okhttp3.",
        "okio.",
        "org.jetbrains.",
        "io.jankhunter.",
    )
}

internal object InstrumentationHooks {
    fun isOkHttpEventListenerFactory(owner: String, name: String, descriptor: String): Boolean {
        return owner == "okhttp3/OkHttpClient\$Builder" &&
            name == "eventListenerFactory" &&
            descriptor == "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;"
    }

    fun isOkHttpBuild(owner: String, name: String, descriptor: String): Boolean {
        return owner == "okhttp3/OkHttpClient\$Builder" &&
            name == "build" &&
            descriptor == "()Lokhttp3/OkHttpClient;"
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
            name == "postAtFrontOfQueue" &&
                descriptor == "(Ljava/lang/Runnable;)Z" -> {
                HandlerRunnableKind.FRONT_RUNNABLE
            }
            name == "postDelayed" &&
                descriptor == "(Ljava/lang/Runnable;J)Z" -> {
                HandlerRunnableKind.RUNNABLE_LONG_DELAY
            }
            name == "postAtTime" &&
                descriptor == "(Ljava/lang/Runnable;J)Z" -> {
                HandlerRunnableKind.RUNNABLE_LONG_TIME
            }
            name == "postDelayed" &&
                descriptor == "(Ljava/lang/Runnable;Ljava/lang/Object;J)Z" -> {
                HandlerRunnableKind.RUNNABLE_OBJECT_LONG_DELAY
            }
            name == "postAtTime" &&
                descriptor == "(Ljava/lang/Runnable;Ljava/lang/Object;J)Z" -> {
                HandlerRunnableKind.RUNNABLE_OBJECT_LONG_TIME
            }
            else -> null
        }
    }

    fun handlerRemoveCallbacksKind(owner: String, name: String, descriptor: String): HandlerRemoveCallbacksKind? {
        if (owner != "android/os/Handler" || name != "removeCallbacks") return null
        return when (descriptor) {
            "(Ljava/lang/Runnable;)V" -> HandlerRemoveCallbacksKind.RUNNABLE
            "(Ljava/lang/Runnable;Ljava/lang/Object;)V" -> HandlerRemoveCallbacksKind.RUNNABLE_OBJECT
            else -> null
        }
    }

    fun isHandlerRemoveCallbacksAndMessages(owner: String, name: String, descriptor: String): Boolean {
        return owner == "android/os/Handler" &&
            name == "removeCallbacksAndMessages" &&
            descriptor == "(Ljava/lang/Object;)V"
    }

    fun isHandlerHasCallbacks(owner: String, name: String, descriptor: String): Boolean {
        return owner == "android/os/Handler" &&
            name == "hasCallbacks" &&
            descriptor == "(Ljava/lang/Runnable;)Z"
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
            name == "submit" &&
                descriptor == "(Ljava/lang/Runnable;)Ljava/util/concurrent/Future;" -> {
                ExecutorRunnableKind.SINGLE_RUNNABLE
            }
            name == "submit" &&
                descriptor == "(Ljava/lang/Runnable;Ljava/lang/Object;)Ljava/util/concurrent/Future;" -> {
                ExecutorRunnableKind.RUNNABLE_OBJECT
            }
            name == "schedule" &&
                descriptor == RUNNABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE -> {
                ExecutorRunnableKind.RUNNABLE_LONG_OBJECT
            }
            name == "scheduleAtFixedRate" &&
                descriptor == RUNNABLE_LONG_LONG_TIME_UNIT_SCHEDULED_FUTURE -> {
                ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT
            }
            name == "scheduleWithFixedDelay" &&
                descriptor == RUNNABLE_LONG_LONG_TIME_UNIT_SCHEDULED_FUTURE -> {
                ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT
            }
            else -> null
        }
    }

    fun executorCallableKind(owner: String, name: String, descriptor: String): ExecutorCallableKind? {
        if (!isExecutorOwner(owner)) return null
        return when {
            name == "submit" &&
                descriptor == "(Ljava/util/concurrent/Callable;)Ljava/util/concurrent/Future;" -> {
                ExecutorCallableKind.SINGLE_CALLABLE
            }
            name == "schedule" &&
                descriptor == CALLABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE -> {
                ExecutorCallableKind.CALLABLE_LONG_OBJECT
            }
            else -> null
        }
    }

    fun coroutineBlockKind(owner: String, name: String, descriptor: String): CoroutineBlockKind? {
        if (owner !in coroutineBuilderOwners) return null
        return when {
            name in coroutineBuildersWithTopBlock &&
                descriptor.endsWith("Lkotlin/jvm/functions/Function2;)${returnDescriptor(owner, name)}") -> {
                CoroutineBlockKind.TOP_FUNCTION2
            }

            name in coroutineBuildersWithDefaultBlock &&
                descriptor.endsWith(defaultCoroutineDescriptorSuffix(owner, name)) -> {
                CoroutineBlockKind.FUNCTION2_BEFORE_INT_OBJECT
            }

            name in coroutineSuspendBuilders &&
                descriptor.endsWith(SUSPEND_COROUTINE_DESCRIPTOR_SUFFIX) -> {
                CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION
            }

            else -> null
        }
    }

    fun isViewSetOnClickListener(owner: String, name: String, descriptor: String): Boolean {
        return name == "setOnClickListener" &&
            descriptor == "(Landroid/view/View\$OnClickListener;)V"
    }

    fun logSpamLevel(owner: String, name: String): Int? {
        if (owner == "android/util/Log") {
            return when (name) {
                "v" -> 2
                "d" -> 3
                "i" -> 4
                "w" -> 5
                "e" -> 6
                "wtf" -> 7
                else -> null
            }
        }
        if (owner == "timber/log/Timber" || owner == "timber/log/Timber\$Tree") {
            return when (name) {
                "v" -> 2
                "d" -> 3
                "i" -> 4
                "w" -> 5
                "e" -> 6
                "wtf" -> 7
                else -> null
            }
        }
        return null
    }

    fun logSpamSource(owner: String, name: String): String {
        return when (owner) {
            "android/util/Log" -> "android.util.Log.$name"
            "timber/log/Timber" -> "Timber.$name"
            "timber/log/Timber\$Tree" -> "Timber.Tree.$name"
            else -> owner.replace('/', '.') + ".$name"
        }
    }

    private fun returnDescriptor(owner: String, name: String): String {
        return when {
            owner == "kotlinx/coroutines/BuildersKt" && name.startsWith("launch") -> "Lkotlinx/coroutines/Job;"
            owner == "kotlinx/coroutines/BuildersKt" && name.startsWith("async") -> "Lkotlinx/coroutines/Deferred;"
            owner == "kotlinx/coroutines/BuildersKt" && name.startsWith("runBlocking") -> "Ljava/lang/Object;"
            else -> "Ljava/lang/Object;"
        }
    }

    private fun defaultCoroutineDescriptorSuffix(owner: String, name: String): String {
        return "Lkotlin/jvm/functions/Function2;ILjava/lang/Object;)" +
            returnDescriptor(owner, name.removeSuffix("\$default"))
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

    private val coroutineBuilderOwners = setOf(
        "kotlinx/coroutines/BuildersKt",
        "kotlinx/coroutines/CoroutineScopeKt",
        "kotlinx/coroutines/SupervisorKt",
        "kotlinx/coroutines/TimeoutKt",
    )

    private val coroutineBuildersWithTopBlock = setOf(
        "launch",
        "async",
        "runBlocking",
    )

    private val coroutineBuildersWithDefaultBlock = setOf(
        "launch\$default",
        "async\$default",
        "runBlocking\$default",
    )

    private val coroutineSuspendBuilders = setOf(
        "withContext",
        "coroutineScope",
        "supervisorScope",
        "withTimeout",
        "withTimeoutOrNull",
    )

    private const val RUNNABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE =
        "(Ljava/lang/Runnable;JLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;"
    private const val RUNNABLE_LONG_LONG_TIME_UNIT_SCHEDULED_FUTURE =
        "(Ljava/lang/Runnable;JJLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;"
    private const val CALLABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE =
        "(Ljava/util/concurrent/Callable;JLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;"
    private const val SUSPEND_COROUTINE_DESCRIPTOR_SUFFIX =
        "Lkotlin/jvm/functions/Function2;Lkotlin/coroutines/Continuation;)Ljava/lang/Object;"
}

internal enum class HandlerRunnableKind {
    SINGLE_RUNNABLE,
    FRONT_RUNNABLE,
    RUNNABLE_LONG_DELAY,
    RUNNABLE_LONG_TIME,
    RUNNABLE_OBJECT_LONG_DELAY,
    RUNNABLE_OBJECT_LONG_TIME,
}

internal enum class HandlerRemoveCallbacksKind {
    RUNNABLE,
    RUNNABLE_OBJECT,
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

internal enum class CoroutineBlockKind {
    TOP_FUNCTION2,
    FUNCTION2_BEFORE_CONTINUATION,
    FUNCTION2_BEFORE_INT_OBJECT,
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
