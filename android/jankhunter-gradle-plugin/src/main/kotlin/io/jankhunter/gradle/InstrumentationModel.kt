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
            InstrumentationHooks.handlerRunnableKind(call.owner, call.name, call.descriptor)?.let {
                return HookDecision.Matched(HookIntent.HandlerRunnable(it), "android.handler.runnable.${it.name.lowercase()}")
            }
            InstrumentationHooks.handlerRemoveCallbacksKind(call.owner, call.name, call.descriptor)?.let {
                return HookDecision.Matched(HookIntent.HandlerRemoveCallbacks(it), "android.handler.remove.${it.name.lowercase()}")
            }
            if (InstrumentationHooks.isHandlerRemoveCallbacksAndMessages(call.owner, call.name, call.descriptor)) {
                return HookDecision.Matched(
                    HookIntent.HandlerRemoveCallbacksAndMessages,
                    "android.handler.remove_callbacks_and_messages",
                )
            }
            if (InstrumentationHooks.isHandlerHasCallbacks(call.owner, call.name, call.descriptor)) {
                return HookDecision.Matched(HookIntent.HandlerHasCallbacks, "android.handler.has_callbacks")
            }
            if (InstrumentationHooks.isHandlerMessageSend(call.owner, call.name, call.descriptor)) {
                return HookDecision.Matched(HookIntent.HandlerMessageSend, "android.handler.message_send")
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
            InstrumentationHooks.executorRunnableKind(call.owner, call.name, call.descriptor)?.let {
                return HookDecision.Matched(HookIntent.ExecutorRunnable(it), "jdk.executor.runnable.${it.name.lowercase()}")
            }
            InstrumentationHooks.executorCallableKind(call.owner, call.name, call.descriptor)?.let {
                return HookDecision.Matched(HookIntent.ExecutorCallable(it), "jdk.executor.callable.${it.name.lowercase()}")
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
