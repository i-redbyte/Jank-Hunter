package io.jankhunter.gradle

import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

internal data class CallerMethod(
    val className: String,
    val methodName: String,
    val descriptor: String,
)

internal data class MethodCall(
    val opcode: Int,
    val owner: String,
    val name: String,
    val descriptor: String,
    val isInterface: Boolean,
    val caller: CallerMethod? = null,
    val line: Int? = null,
) {
    val invocationKind: InvocationKind
        get() = InvocationKind.fromOpcode(opcode)

    fun arguments(startSlot: Int = 0): DescriptorArgumentIterator {
        return DescriptorArgumentIterator(descriptor, startSlot)
    }
}

internal enum class InvocationKind {
    Static,
    Virtual,
    Interface,
    Special,
    Dynamic,
    Unknown,
    ;

    companion object {
        fun fromOpcode(opcode: Int): InvocationKind {
            return when (opcode and SOURCE_MASK.inv()) {
                Opcodes.INVOKESTATIC -> Static
                Opcodes.INVOKEVIRTUAL -> Virtual
                Opcodes.INVOKEINTERFACE -> Interface
                Opcodes.INVOKESPECIAL -> Special
                Opcodes.INVOKEDYNAMIC -> Dynamic
                else -> Unknown
            }
        }

        private const val SOURCE_MASK = Opcodes.SOURCE_MASK
    }
}

internal data class DescriptorArgument(
    val index: Int,
    val type: Type,
    val slotIndex: Int,
) {
    val size: Int
        get() = type.size
}

internal class DescriptorArgumentIterator(
    descriptor: String,
    private val startSlot: Int = 0,
) : Iterator<DescriptorArgument>, Iterable<DescriptorArgument> {
    private val types = Type.getArgumentTypes(descriptor)
    private var index = 0
    private var nextSlot = startSlot

    override fun iterator(): Iterator<DescriptorArgument> = this

    override fun hasNext(): Boolean = index < types.size

    override fun next(): DescriptorArgument {
        if (!hasNext()) throw NoSuchElementException()
        val type = types[index]
        val argument = DescriptorArgument(
            index = index,
            type = type,
            slotIndex = nextSlot,
        )
        index += 1
        nextSlot += type.size
        return argument
    }
}

internal enum class ArgumentRole {
    Builder,
    Listener,
    Runnable,
    Callable,
    Executor,
    Continuation,
    Owner,
    Route,
    Screen,
    Token,
    Delay,
    Period,
    TimeUnit,
}

internal data class SignatureSpec(
    val id: String,
    val owners: Set<String>,
    val names: Set<String>,
    val descriptors: Set<String>,
    val roles: Map<ArgumentRole, Int> = emptyMap(),
) {
    constructor(
        id: String,
        owner: String,
        name: String,
        descriptor: String,
        roles: Map<ArgumentRole, Int> = emptyMap(),
    ) : this(
        id = id,
        owners = setOf(owner),
        names = setOf(name),
        descriptors = setOf(descriptor),
        roles = roles,
    )

    fun matches(call: MethodCall): Boolean {
        return call.owner in owners &&
            call.name in names &&
            call.descriptor in descriptors
    }
}

internal data class IntentSignature(
    val spec: SignatureSpec,
    val intent: HookIntent,
)

internal sealed class HookIntent(
    val id: String,
) {
    data object WrapOkHttpEventListenerFactory : HookIntent("okhttp.wrap_event_listener_factory")
    data object InstallOkHttpEventListenerFactory : HookIntent("okhttp.install_event_listener_factory")
    data object WrapWebSocketListener : HookIntent("okhttp.wrap_websocket_listener")
    data class HandlerRunnable(val kind: HandlerRunnableKind) : HookIntent("handler.wrap_runnable.${kind.name.lowercase()}")
    data class HandlerRemoveCallbacks(
        val kind: HandlerRemoveCallbacksKind,
    ) : HookIntent("handler.remove_callbacks.${kind.name.lowercase()}")
    data object HandlerRemoveCallbacksAndMessages : HookIntent("handler.remove_callbacks_and_messages")
    data object HandlerHasCallbacks : HookIntent("handler.has_callbacks")
    data object HandlerMessageSend : HookIntent("handler.send_message")
    data class ExecutorRunnable(val kind: ExecutorRunnableKind) : HookIntent("executor.wrap_runnable.${kind.name.lowercase()}")
    data class ExecutorCallable(val kind: ExecutorCallableKind) : HookIntent("executor.wrap_callable.${kind.name.lowercase()}")
    data class CoroutineBlock(val kind: CoroutineBlockKind) : HookIntent("coroutine.wrap_block.${kind.name.lowercase()}")
    data object WrapClickListener : HookIntent("flow.wrap_click_listener")
    data class LogSpam(val source: String, val level: Int) : HookIntent("logspam.$source")
}

internal interface InstrumentationRule {
    val id: String
    val priority: Int
    fun evaluate(call: MethodCall, config: HookConfig): HookDecision
}

internal sealed class HookDecision {
    data class Matched(
        val intent: HookIntent,
        val signatureId: String,
    ) : HookDecision()

    data object NotMatched : HookDecision()
}

internal object HookSignatureCatalog {
    private const val RUNNABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE =
        "(Ljava/lang/Runnable;JLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;"
    private const val RUNNABLE_LONG_LONG_TIME_UNIT_SCHEDULED_FUTURE =
        "(Ljava/lang/Runnable;JJLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;"
    private const val CALLABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE =
        "(Ljava/util/concurrent/Callable;JLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;"

    val okHttpEventListenerFactory = SignatureSpec(
        id = "okhttp3.builder.event_listener_factory.v3",
        owner = "okhttp3/OkHttpClient\$Builder",
        name = "eventListenerFactory",
        descriptor = "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
        roles = mapOf(ArgumentRole.Listener to 0),
    )

    val okHttpBuild = SignatureSpec(
        id = "okhttp3.builder.build.v3",
        owner = "okhttp3/OkHttpClient\$Builder",
        name = "build",
        descriptor = "()Lokhttp3/OkHttpClient;",
    )

    val okHttpNewWebSocket = SignatureSpec(
        id = "okhttp3.client.new_websocket.v3",
        owner = "okhttp3/OkHttpClient",
        name = "newWebSocket",
        descriptor = "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
        roles = mapOf(ArgumentRole.Listener to 1),
    )

    val handlerRunnableSignatures = listOf(
        IntentSignature(
            SignatureSpec(
                id = "android.handler.post.runnable",
                owner = "android/os/Handler",
                name = "post",
                descriptor = "(Ljava/lang/Runnable;)Z",
                roles = mapOf(ArgumentRole.Runnable to 0),
            ),
            HookIntent.HandlerRunnable(HandlerRunnableKind.SINGLE_RUNNABLE),
        ),
        IntentSignature(
            SignatureSpec(
                id = "android.handler.post_at_front.runnable",
                owner = "android/os/Handler",
                name = "postAtFrontOfQueue",
                descriptor = "(Ljava/lang/Runnable;)Z",
                roles = mapOf(ArgumentRole.Runnable to 0),
            ),
            HookIntent.HandlerRunnable(HandlerRunnableKind.FRONT_RUNNABLE),
        ),
        IntentSignature(
            SignatureSpec(
                id = "android.handler.post_delayed.runnable_delay",
                owner = "android/os/Handler",
                name = "postDelayed",
                descriptor = "(Ljava/lang/Runnable;J)Z",
                roles = mapOf(ArgumentRole.Runnable to 0, ArgumentRole.Delay to 1),
            ),
            HookIntent.HandlerRunnable(HandlerRunnableKind.RUNNABLE_LONG_DELAY),
        ),
        IntentSignature(
            SignatureSpec(
                id = "android.handler.post_at_time.runnable_time",
                owner = "android/os/Handler",
                name = "postAtTime",
                descriptor = "(Ljava/lang/Runnable;J)Z",
                roles = mapOf(ArgumentRole.Runnable to 0, ArgumentRole.Delay to 1),
            ),
            HookIntent.HandlerRunnable(HandlerRunnableKind.RUNNABLE_LONG_TIME),
        ),
        IntentSignature(
            SignatureSpec(
                id = "android.handler.post_delayed.runnable_token_delay",
                owner = "android/os/Handler",
                name = "postDelayed",
                descriptor = "(Ljava/lang/Runnable;Ljava/lang/Object;J)Z",
                roles = mapOf(ArgumentRole.Runnable to 0, ArgumentRole.Token to 1, ArgumentRole.Delay to 2),
            ),
            HookIntent.HandlerRunnable(HandlerRunnableKind.RUNNABLE_OBJECT_LONG_DELAY),
        ),
        IntentSignature(
            SignatureSpec(
                id = "android.handler.post_at_time.runnable_token_time",
                owner = "android/os/Handler",
                name = "postAtTime",
                descriptor = "(Ljava/lang/Runnable;Ljava/lang/Object;J)Z",
                roles = mapOf(ArgumentRole.Runnable to 0, ArgumentRole.Token to 1, ArgumentRole.Delay to 2),
            ),
            HookIntent.HandlerRunnable(HandlerRunnableKind.RUNNABLE_OBJECT_LONG_TIME),
        ),
    )

    val handlerRemoveCallbacksSignatures = listOf(
        IntentSignature(
            SignatureSpec(
                id = "android.handler.remove_callbacks.runnable",
                owner = "android/os/Handler",
                name = "removeCallbacks",
                descriptor = "(Ljava/lang/Runnable;)V",
                roles = mapOf(ArgumentRole.Runnable to 0),
            ),
            HookIntent.HandlerRemoveCallbacks(HandlerRemoveCallbacksKind.RUNNABLE),
        ),
        IntentSignature(
            SignatureSpec(
                id = "android.handler.remove_callbacks.runnable_token",
                owner = "android/os/Handler",
                name = "removeCallbacks",
                descriptor = "(Ljava/lang/Runnable;Ljava/lang/Object;)V",
                roles = mapOf(ArgumentRole.Runnable to 0, ArgumentRole.Token to 1),
            ),
            HookIntent.HandlerRemoveCallbacks(HandlerRemoveCallbacksKind.RUNNABLE_OBJECT),
        ),
    )

    val handlerRemoveCallbacksAndMessages = IntentSignature(
        SignatureSpec(
            id = "android.handler.remove_callbacks_and_messages.token",
            owner = "android/os/Handler",
            name = "removeCallbacksAndMessages",
            descriptor = "(Ljava/lang/Object;)V",
            roles = mapOf(ArgumentRole.Token to 0),
        ),
        HookIntent.HandlerRemoveCallbacksAndMessages,
    )

    val handlerHasCallbacks = IntentSignature(
        SignatureSpec(
            id = "android.handler.has_callbacks.runnable",
            owner = "android/os/Handler",
            name = "hasCallbacks",
            descriptor = "(Ljava/lang/Runnable;)Z",
            roles = mapOf(ArgumentRole.Runnable to 0),
        ),
        HookIntent.HandlerHasCallbacks,
    )

    val handlerMessageSendSignatures = listOf(
        SignatureSpec(
            id = "android.handler.send_message.message",
            owners = setOf("android/os/Handler"),
            names = setOf("sendMessage", "sendMessageAtFrontOfQueue"),
            descriptors = setOf("(Landroid/os/Message;)Z"),
        ),
        SignatureSpec(
            id = "android.handler.send_message_delayed.message_time",
            owners = setOf("android/os/Handler"),
            names = setOf("sendMessageDelayed", "sendMessageAtTime"),
            descriptors = setOf("(Landroid/os/Message;J)Z"),
            roles = mapOf(ArgumentRole.Delay to 1),
        ),
    )

    val executorOwners = setOf(
        "java/util/concurrent/Executor",
        "java/util/concurrent/ExecutorService",
        "java/util/concurrent/ScheduledExecutorService",
        "java/util/concurrent/AbstractExecutorService",
        "java/util/concurrent/ThreadPoolExecutor",
        "java/util/concurrent/ScheduledThreadPoolExecutor",
        "java/util/concurrent/ForkJoinPool",
    )

    val executorRunnableSignatures = listOf(
        IntentSignature(
            SignatureSpec(
                id = "jdk.executor.execute.runnable",
                owners = executorOwners,
                names = setOf("execute"),
                descriptors = setOf("(Ljava/lang/Runnable;)V"),
                roles = mapOf(ArgumentRole.Runnable to 0),
            ),
            HookIntent.ExecutorRunnable(ExecutorRunnableKind.SINGLE_RUNNABLE),
        ),
        IntentSignature(
            SignatureSpec(
                id = "jdk.executor.submit.runnable",
                owners = executorOwners,
                names = setOf("submit"),
                descriptors = setOf("(Ljava/lang/Runnable;)Ljava/util/concurrent/Future;"),
                roles = mapOf(ArgumentRole.Runnable to 0),
            ),
            HookIntent.ExecutorRunnable(ExecutorRunnableKind.SINGLE_RUNNABLE),
        ),
        IntentSignature(
            SignatureSpec(
                id = "jdk.executor.submit.runnable_result",
                owners = executorOwners,
                names = setOf("submit"),
                descriptors = setOf("(Ljava/lang/Runnable;Ljava/lang/Object;)Ljava/util/concurrent/Future;"),
                roles = mapOf(ArgumentRole.Runnable to 0),
            ),
            HookIntent.ExecutorRunnable(ExecutorRunnableKind.RUNNABLE_OBJECT),
        ),
        IntentSignature(
            SignatureSpec(
                id = "jdk.scheduled_executor.schedule.runnable_delay_unit",
                owners = executorOwners,
                names = setOf("schedule"),
                descriptors = setOf(RUNNABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE),
                roles = mapOf(ArgumentRole.Runnable to 0, ArgumentRole.Delay to 1, ArgumentRole.TimeUnit to 2),
            ),
            HookIntent.ExecutorRunnable(ExecutorRunnableKind.RUNNABLE_LONG_OBJECT),
        ),
        IntentSignature(
            SignatureSpec(
                id = "jdk.scheduled_executor.periodic.runnable_delay_period_unit",
                owners = executorOwners,
                names = setOf("scheduleAtFixedRate", "scheduleWithFixedDelay"),
                descriptors = setOf(RUNNABLE_LONG_LONG_TIME_UNIT_SCHEDULED_FUTURE),
                roles = mapOf(
                    ArgumentRole.Runnable to 0,
                    ArgumentRole.Delay to 1,
                    ArgumentRole.Period to 2,
                    ArgumentRole.TimeUnit to 3,
                ),
            ),
            HookIntent.ExecutorRunnable(ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT),
        ),
    )

    val executorCallableSignatures = listOf(
        IntentSignature(
            SignatureSpec(
                id = "jdk.executor.submit.callable",
                owners = executorOwners,
                names = setOf("submit"),
                descriptors = setOf("(Ljava/util/concurrent/Callable;)Ljava/util/concurrent/Future;"),
                roles = mapOf(ArgumentRole.Callable to 0),
            ),
            HookIntent.ExecutorCallable(ExecutorCallableKind.SINGLE_CALLABLE),
        ),
        IntentSignature(
            SignatureSpec(
                id = "jdk.scheduled_executor.schedule.callable_delay_unit",
                owners = executorOwners,
                names = setOf("schedule"),
                descriptors = setOf(CALLABLE_LONG_TIME_UNIT_SCHEDULED_FUTURE),
                roles = mapOf(ArgumentRole.Callable to 0, ArgumentRole.Delay to 1, ArgumentRole.TimeUnit to 2),
            ),
            HookIntent.ExecutorCallable(ExecutorCallableKind.CALLABLE_LONG_OBJECT),
        ),
    )

    fun matchIntent(call: MethodCall, signatures: List<IntentSignature>): HookDecision.Matched? {
        val matched = signatures.firstOrNull { it.spec.matches(call) } ?: return null
        return HookDecision.Matched(matched.intent, matched.spec.id)
    }
}

internal class RuleRegistry(
    rules: List<InstrumentationRule>,
) {
    private val rules = rules.sortedWith(compareBy<InstrumentationRule> { it.priority }.thenBy { it.id })

    fun resolve(call: MethodCall, config: HookConfig): HookDecision {
        rules.forEach { rule ->
            val decision = rule.evaluate(call, config)
            if (decision is HookDecision.Matched) return decision
        }
        return HookDecision.NotMatched
    }
}

internal object HookIntentResolver {
    private val registry = RuleRegistry(
        listOf(
            OkHttpInstrumentationRule,
            WebSocketInstrumentationRule,
            HandlerInstrumentationRule,
            ExecutorInstrumentationRule,
            CoroutineInstrumentationRule,
            FlowInstrumentationRule,
            LogSpamInstrumentationRule,
        ),
    )

    fun resolve(call: MethodCall, config: HookConfig): HookDecision {
        return registry.resolve(call, config)
    }
}

private object OkHttpInstrumentationRule : InstrumentationRule {
    override val id: String = "okhttp"
    override val priority: Int = 100

    override fun evaluate(call: MethodCall, config: HookConfig): HookDecision {
        if (config.okhttp) {
            when {
                HookSignatureCatalog.okHttpEventListenerFactory.matches(call) -> {
                    return HookDecision.Matched(
                        HookIntent.WrapOkHttpEventListenerFactory,
                        HookSignatureCatalog.okHttpEventListenerFactory.id,
                    )
                }
                HookSignatureCatalog.okHttpBuild.matches(call) -> {
                    return HookDecision.Matched(
                        HookIntent.InstallOkHttpEventListenerFactory,
                        HookSignatureCatalog.okHttpBuild.id,
                    )
                }
            }
        }
        return HookDecision.NotMatched
    }
}

private object WebSocketInstrumentationRule : InstrumentationRule {
    override val id: String = "websocket"
    override val priority: Int = 110

    override fun evaluate(call: MethodCall, config: HookConfig): HookDecision {
        if (config.webSockets && HookSignatureCatalog.okHttpNewWebSocket.matches(call)) {
            return HookDecision.Matched(
                HookIntent.WrapWebSocketListener,
                HookSignatureCatalog.okHttpNewWebSocket.id,
            )
        }
        return HookDecision.NotMatched
    }
}

private object HandlerInstrumentationRule : InstrumentationRule {
    override val id: String = "handler"
    override val priority: Int = 200

    override fun evaluate(call: MethodCall, config: HookConfig): HookDecision {
        if (config.handlers) {
            HookSignatureCatalog.matchIntent(call, HookSignatureCatalog.handlerRunnableSignatures)?.let {
                return it
            }
            HookSignatureCatalog.matchIntent(call, HookSignatureCatalog.handlerRemoveCallbacksSignatures)?.let {
                return it
            }
            if (HookSignatureCatalog.handlerRemoveCallbacksAndMessages.spec.matches(call)) {
                return HookDecision.Matched(
                    HookIntent.HandlerRemoveCallbacksAndMessages,
                    HookSignatureCatalog.handlerRemoveCallbacksAndMessages.spec.id,
                )
            }
            if (HookSignatureCatalog.handlerHasCallbacks.spec.matches(call)) {
                return HookDecision.Matched(HookIntent.HandlerHasCallbacks, HookSignatureCatalog.handlerHasCallbacks.spec.id)
            }
            HookSignatureCatalog.handlerMessageSendSignatures.firstOrNull { it.matches(call) }?.let {
                return HookDecision.Matched(HookIntent.HandlerMessageSend, it.id)
            }
        }
        return HookDecision.NotMatched
    }
}

private object ExecutorInstrumentationRule : InstrumentationRule {
    override val id: String = "executor"
    override val priority: Int = 300

    override fun evaluate(call: MethodCall, config: HookConfig): HookDecision {
        if (config.executors) {
            HookSignatureCatalog.matchIntent(call, HookSignatureCatalog.executorRunnableSignatures)?.let {
                return it
            }
            HookSignatureCatalog.matchIntent(call, HookSignatureCatalog.executorCallableSignatures)?.let {
                return it
            }
        }
        return HookDecision.NotMatched
    }
}

private object CoroutineInstrumentationRule : InstrumentationRule {
    override val id: String = "coroutine"
    override val priority: Int = 400

    override fun evaluate(call: MethodCall, config: HookConfig): HookDecision {
        if (config.coroutines) {
            InstrumentationHooks.coroutineBlockKind(call.owner, call.name, call.descriptor)?.let {
                return HookDecision.Matched(HookIntent.CoroutineBlock(it), "kotlin.coroutines.block.${it.name.lowercase()}")
            }
        }
        return HookDecision.NotMatched
    }
}

private object FlowInstrumentationRule : InstrumentationRule {
    override val id: String = "flow"
    override val priority: Int = 500

    override fun evaluate(call: MethodCall, config: HookConfig): HookDecision {
        if (config.flowInteractions && InstrumentationHooks.isViewSetOnClickListener(call.owner, call.name, call.descriptor)) {
            return HookDecision.Matched(HookIntent.WrapClickListener, "android.view.click_listener")
        }
        return HookDecision.NotMatched
    }
}

private object LogSpamInstrumentationRule : InstrumentationRule {
    override val id: String = "logspam"
    override val priority: Int = 600

    override fun evaluate(call: MethodCall, config: HookConfig): HookDecision {
        if (config.logSpam) {
            val level = InstrumentationHooks.logSpamLevel(call.owner, call.name)
            if (level != null) {
                val source = InstrumentationHooks.logSpamSource(call.owner, call.name)
                return HookDecision.Matched(HookIntent.LogSpam(source, level), "logspam.$source")
            }
        }
        return HookDecision.NotMatched
    }
}
