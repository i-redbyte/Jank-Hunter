package io.jankhunter.gradle

import org.objectweb.asm.Type

internal data class CallerMethod(
    val className: String,
    val methodName: String,
    val descriptor: String,
)

internal data class MethodCall(
    val owner: String,
    val name: String,
    val descriptor: String,
    val caller: CallerMethod? = null,
    val line: Int? = null,
    val ownerHierarchy: Set<String> = setOf(owner),
    val databaseQuery: String? = null,
    val databaseQueryArgument: Int? = null,
)

internal fun MethodCall.matchesOwner(owners: Set<String>): Boolean {
    return ownerHierarchy.any(owners::contains)
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

internal enum class CriticalIOCallKind {
    FILE_READ_BYTES,
    FILE_WRITE_BYTES,
    FILE_APPEND_BYTES,
    FILE_DESCRIPTOR_SYNC,
    FILE_CHANNEL_FORCE,
}

internal enum class DatabaseFrameworkKind(val wireValue: Int) {
    SQLITE(1),
    SUPPORT_SQLITE(2),
    ROOM(3),
}

internal enum class DatabaseOperationKind(val wireValue: Int) {
    QUERY(1),
    INSERT(2),
    UPDATE(3),
    DELETE(4),
    EXECUTE(5),
    STATEMENT(6),
}

internal enum class DatabaseBoundaryKind(val wireValue: Int) {
    DISPATCH(1),
    EXECUTE(2),
    MATERIALIZE(3),
    MANUAL(4),
}

internal enum class DatabaseResultCapture(val wireValue: Int, val resultKindWireValue: Int) {
    NONE(0, 0),
    INSERT_ROW_ID(1, 2),
    AFFECTED_ROWS(2, 2),
}

internal enum class DatabaseStatementAction {
    NONE,
    REGISTER,
    EXECUTE,
}

internal enum class DatabaseTransactionAction {
    BEGIN,
    MARK_SUCCESSFUL,
    END,
}

internal enum class DatabaseTransactionModeKind(val wireValue: Int) {
    UNKNOWN(0),
    DEFERRED(1),
    IMMEDIATE(2),
    EXCLUSIVE(3),
    READ_ONLY(4),
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
        return call.matchesOwner(owners) &&
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
    data object InstallOkHttpEventListener : HookIntent("okhttp.install_event_listener")
    data object InstallOkHttpEventListenerFactory : HookIntent("okhttp.install_event_listener_factory")
    data object GuardOkHttpNewCall : HookIntent("okhttp.guard_new_call")
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
    data object WrapClickListener : HookIntent("interaction_operation.wrap_click_listener")
    data class LogSpam(val source: String, val level: Int) : HookIntent("logspam.$source")
    data class CriticalIO(val kind: CriticalIOCallKind) : HookIntent("io.${kind.name.lowercase()}")
    data class DatabaseCall(
        val framework: DatabaseFrameworkKind,
        val operation: DatabaseOperationKind,
        val query: String? = null,
        val queryArgument: Int? = null,
        val boundary: DatabaseBoundaryKind = DatabaseBoundaryKind.EXECUTE,
        val resultCapture: DatabaseResultCapture = DatabaseResultCapture.NONE,
        val statementAction: DatabaseStatementAction = DatabaseStatementAction.NONE,
    ) : HookIntent("database.${framework.name.lowercase()}.${operation.name.lowercase()}")
    data class DatabaseTransaction(
        val framework: DatabaseFrameworkKind,
        val action: DatabaseTransactionAction,
        val mode: DatabaseTransactionModeKind,
    ) : HookIntent("database.transaction.${framework.name.lowercase()}.${action.name.lowercase()}")
}

internal interface InstrumentationModule {
    val id: String
    val family: String
    val priority: Int
    val bridges: List<VersionedInstrumentationBridge>
    val needsControlFlow: Boolean
        get() = false

    fun enabled(config: HookConfig): Boolean

    fun relevant(call: MethodCall): Boolean {
        return bridges.any { it.relevant(call) }
    }

    fun match(call: MethodCall, config: HookConfig): HookDecision {
        if (!relevant(call)) return HookDecision.NotMatched
        if (!enabled(config)) return HookDecision.Disabled(id, family, "disabled_by_gate")
        return bridges.firstNotNullOfOrNull { it.match(call) }?.toDecision()
            ?: HookDecision.Unsupported(id, family, "unsupported_signature")
    }
}

internal sealed class HookDecision {
    data class Matched(
        val intent: HookIntent,
        val signatureId: String,
        val bridgeId: String? = null,
    ) : HookDecision()

    data class Disabled(
        val moduleId: String,
        val family: String,
        val reason: String,
    ) : HookDecision()

    data class Unsupported(
        val moduleId: String,
        val family: String,
        val reason: String,
    ) : HookDecision()

    data class Skipped(
        val moduleId: String,
        val family: String,
        val reason: String,
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

    val okHttpEventListener = SignatureSpec(
        id = "okhttp3.builder.event_listener.v3",
        owner = "okhttp3/OkHttpClient\$Builder",
        name = "eventListener",
        descriptor = "(Lokhttp3/EventListener;)Lokhttp3/OkHttpClient\$Builder;",
        roles = mapOf(ArgumentRole.Listener to 0),
    )

    val okHttpBuild = SignatureSpec(
        id = "okhttp3.builder.build.v3",
        owner = "okhttp3/OkHttpClient\$Builder",
        name = "build",
        descriptor = "()Lokhttp3/OkHttpClient;",
    )

    val okHttpNewCall = SignatureSpec(
        id = "okhttp3.client.new_call.v3",
        owner = "okhttp3/OkHttpClient",
        name = "newCall",
        descriptor = "(Lokhttp3/Request;)Lokhttp3/Call;",
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

}

internal class InstrumentationModuleRegistry(
    modules: List<InstrumentationModule>,
) {
    private val modules = modules.sortedWith(compareBy<InstrumentationModule> { it.priority }.thenBy { it.id })

    fun resolve(call: MethodCall, config: HookConfig): HookDecision {
        var firstDiagnostic: HookDecision? = null
        modules.forEach { module ->
            val decision = module.match(call, config)
            if (decision is HookDecision.Matched) return decision
            if (decision !is HookDecision.NotMatched && firstDiagnostic == null) {
                firstDiagnostic = decision
            }
        }
        return firstDiagnostic ?: HookDecision.NotMatched
    }

    fun modules(): List<InstrumentationModule> = modules

    fun needsControlFlow(): Boolean = modules.any { it.needsControlFlow }

    fun needsControlFlow(config: HookConfig): Boolean {
        return modules.any { it.needsControlFlow && it.enabled(config) }
    }
}

internal object HookIntentResolver {
    private val registry = InstrumentationModuleRegistry(
        listOf(
            OkHttpInstrumentationModule,
            WebSocketInstrumentationModule,
            HandlerInstrumentationModule,
            ExecutorInstrumentationModule,
            CoroutineInstrumentationModule,
            InteractionOperationInstrumentationModule,
            LogSpamInstrumentationModule,
            CriticalIOInstrumentationModule,
            DatabaseInstrumentationModule,
        ),
    )

    fun resolve(call: MethodCall, config: HookConfig): HookDecision {
        return registry.resolve(call, config)
    }

    fun modules(): List<InstrumentationModule> = registry.modules()

    fun needsControlFlow(): Boolean = registry.needsControlFlow()

    fun needsControlFlow(config: HookConfig): Boolean = registry.needsControlFlow(config)
}

internal object HookNearMissDiagnostics {
    fun resolve(call: MethodCall, config: HookConfig): HookDecision.Skipped? {
        return coroutineNearMiss(call, config)
            ?: okHttpNearMiss(call, config)
    }

    private fun coroutineNearMiss(call: MethodCall, config: HookConfig): HookDecision.Skipped? {
        if (!config.coroutines) return null
        if (!call.owner.startsWith("kotlinx/coroutines/")) return null
        val knownBuilderName = call.name.removeSuffix("\$default") in coroutineBuilderNames
        val descriptorLooksRelevant = call.descriptor.contains("Lkotlin/jvm/functions/Function2;") ||
            call.descriptor.contains("Lkotlin/coroutines/Continuation;")
        if (!knownBuilderName && !descriptorLooksRelevant) return null
        return HookDecision.Skipped("coroutine", "coroutines", "near_miss_coroutine_signature")
    }

    private fun okHttpNearMiss(call: MethodCall, config: HookConfig): HookDecision.Skipped? {
        if (!call.owner.startsWith("okhttp3/")) return null
        if (config.webSockets && call.name == "newWebSocket") {
            return HookDecision.Skipped("websocket", "okhttp", "near_miss_okhttp_signature")
        }
        if (config.okhttp && call.name in okHttpBuilderNames) {
            return HookDecision.Skipped("okhttp", "okhttp", "near_miss_okhttp_signature")
        }
        return null
    }

    private val coroutineBuilderNames = setOf(
        "launch",
        "async",
        "runBlocking",
        "withContext",
        "coroutineScope",
        "supervisorScope",
        "withTimeout",
        "withTimeoutOrNull",
    )
    private val okHttpBuilderNames = setOf("eventListener", "eventListenerFactory", "build", "newCall")
}

private object OkHttpInstrumentationModule : InstrumentationModule {
    override val id: String = "okhttp"
    override val family: String = "okhttp"
    override val priority: Int = 100
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.okHttp
    private val intents = setOf(
        HookIntent.WrapOkHttpEventListenerFactory.id,
        HookIntent.InstallOkHttpEventListener.id,
        HookIntent.InstallOkHttpEventListenerFactory.id,
        HookIntent.GuardOkHttpNewCall.id,
    )

    override fun enabled(config: HookConfig): Boolean = config.okhttp

    override fun relevant(call: MethodCall): Boolean = relevantOkHttp(call, intents)

    override fun match(call: MethodCall, config: HookConfig): HookDecision {
        return matchOkHttpModule(this, call, config, intents)
    }
}

private object WebSocketInstrumentationModule : InstrumentationModule {
    override val id: String = "websocket"
    override val family: String = "okhttp"
    override val priority: Int = 110
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.okHttp
    private val intents = setOf(HookIntent.WrapWebSocketListener.id)

    override fun enabled(config: HookConfig): Boolean = config.webSockets

    override fun relevant(call: MethodCall): Boolean = relevantOkHttp(call, intents)

    override fun match(call: MethodCall, config: HookConfig): HookDecision {
        return matchOkHttpModule(this, call, config, intents)
    }
}

private fun matchOkHttpModule(
    module: InstrumentationModule,
    call: MethodCall,
    config: HookConfig,
    intents: Set<String>,
): HookDecision {
    if (!module.relevant(call)) return HookDecision.NotMatched
    if (!module.enabled(config)) return HookDecision.Disabled(module.id, module.family, "disabled_by_gate")
    return VersionedBridgeCatalog.matchOkHttp(call, intents)?.toDecision()
        ?: HookDecision.Unsupported(module.id, module.family, "unsupported_signature")
}

private fun relevantOkHttp(call: MethodCall, intents: Set<String>): Boolean {
    return VersionedBridgeCatalog.okHttp.any { bridge ->
        bridge.signatures.any { signature ->
            call.owner in signature.owners &&
                call.name in signature.names &&
                signature.intent.id in intents
        }
    }
}

private object HandlerInstrumentationModule : InstrumentationModule {
    override val id: String = "handler"
    override val family: String = "handler"
    override val priority: Int = 200
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.handlers

    override fun enabled(config: HookConfig): Boolean = config.handlers
}

private object ExecutorInstrumentationModule : InstrumentationModule {
    override val id: String = "executor"
    override val family: String = "executor"
    override val priority: Int = 300
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.executors

    override fun enabled(config: HookConfig): Boolean = config.executors
}

private object CoroutineInstrumentationModule : InstrumentationModule {
    override val id: String = "coroutine"
    override val family: String = "coroutines"
    override val priority: Int = 400
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.coroutines

    override fun enabled(config: HookConfig): Boolean = config.coroutines
}

private object InteractionOperationInstrumentationModule : InstrumentationModule {
    override val id: String = "interaction-operation"
    override val family: String = "interaction-operation"
    override val priority: Int = 500
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.interactionOperations

    override fun enabled(config: HookConfig): Boolean = config.interactionOperations
}

private object LogSpamInstrumentationModule : InstrumentationModule {
    override val id: String = "logspam"
    override val family: String = "logspam"
    override val priority: Int = 600
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.logSpam

    override fun enabled(config: HookConfig): Boolean = config.logSpam
}

private object CriticalIOInstrumentationModule : InstrumentationModule {
    override val id: String = "critical_io"
    override val family: String = "io"
    override val priority: Int = 700
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.io

    override fun enabled(config: HookConfig): Boolean = config.ioTracing
}

private object DatabaseInstrumentationModule : InstrumentationModule {
    override val id: String = "database"
    override val family: String = "database"
    override val priority: Int = 710
    override val bridges: List<VersionedInstrumentationBridge> = VersionedBridgeCatalog.database
    override val needsControlFlow: Boolean = true

    override fun enabled(config: HookConfig): Boolean = config.databaseTracing

    override fun relevant(call: MethodCall): Boolean {
        return databaseIntent(call) != null || bridges.any { it.relevant(call) }
    }

    override fun match(call: MethodCall, config: HookConfig): HookDecision {
        val intent = databaseIntent(call) ?: return HookDecision.NotMatched
        if (!enabled(config)) return HookDecision.Disabled(id, family, "disabled_by_gate")
        val matched = bridges.firstNotNullOfOrNull { it.match(call) }
            ?: return HookDecision.Unsupported(id, family, "unsupported_signature")
        return HookDecision.Matched(intent, matched.signature.id, matched.bridgeId)
    }
}

private fun databaseIntent(call: MethodCall): HookIntent? {
    if (call.caller?.className?.let(::isDatabaseImplementationClass) == true) return null
    val framework = databaseFramework(call) ?: return null
    return databaseTransactionIntent(call, framework) ?: databaseCallIntent(call, framework)
}

private fun databaseCallIntent(
    call: MethodCall,
    framework: DatabaseFrameworkKind,
): HookIntent.DatabaseCall? {
    val operation = when {
        call.name.startsWith("rawQuery") || call.name == "query" || call.name.startsWith("simpleQueryFor") ->
            DatabaseOperationKind.QUERY
        call.name.startsWith("insert") -> DatabaseOperationKind.INSERT
        call.name.startsWith("update") -> DatabaseOperationKind.UPDATE
        call.name.startsWith("delete") -> DatabaseOperationKind.DELETE
        call.name in setOf("execSQL", "execute", "executeInsert", "executeUpdateDelete") ->
            DatabaseOperationKind.EXECUTE
        call.name == "compileStatement" -> DatabaseOperationKind.STATEMENT
        call.matchesOwner(DATABASE_ROOM_INSERT_OWNERS) && call.name.startsWith("insert") ->
            DatabaseOperationKind.INSERT
        call.matchesOwner(DATABASE_ROOM_UPSERT_OWNERS) && call.name.startsWith("upsert") ->
            DatabaseOperationKind.INSERT
        call.matchesOwner(DATABASE_ROOM_MUTATION_OWNERS) && call.name.startsWith("handle") ->
            DatabaseOperationKind.EXECUTE
        else -> return null
    }
    val statementAction = when {
        call.name == "compileStatement" -> DatabaseStatementAction.REGISTER
        call.matchesOwner(DATABASE_STATEMENT_OWNERS) -> DatabaseStatementAction.EXECUTE
        else -> DatabaseStatementAction.NONE
    }
    val resultCapture = when {
        call.name == "executeInsert" ||
            (call.name.startsWith("insert") || call.name.startsWith("upsert")) &&
            Type.getReturnType(call.descriptor) == Type.LONG_TYPE ->
            DatabaseResultCapture.INSERT_ROW_ID
        call.name == "executeUpdateDelete" ||
            (call.name.startsWith("update") || call.name.startsWith("delete") || call.name.startsWith("handle")) &&
            Type.getReturnType(call.descriptor) == Type.INT_TYPE -> DatabaseResultCapture.AFFECTED_ROWS
        else -> DatabaseResultCapture.NONE
    }
    return HookIntent.DatabaseCall(
        framework,
        operation,
        call.databaseQuery,
        call.databaseQueryArgument ?: databaseQueryArgumentIndex(call, framework),
        if (operation == DatabaseOperationKind.QUERY) DatabaseBoundaryKind.DISPATCH else DatabaseBoundaryKind.EXECUTE,
        resultCapture,
        statementAction,
    )
}

private fun databaseTransactionIntent(
    call: MethodCall,
    framework: DatabaseFrameworkKind,
): HookIntent.DatabaseTransaction? {
    if (!call.matchesOwner(DATABASE_TRANSACTION_OWNERS) || Type.getReturnType(call.descriptor) != Type.VOID_TYPE) {
        return null
    }
    val action: DatabaseTransactionAction
    val mode: DatabaseTransactionModeKind
    when (call.name) {
        "beginTransaction", "beginTransactionWithListener" -> {
            action = DatabaseTransactionAction.BEGIN
            mode = DatabaseTransactionModeKind.EXCLUSIVE
        }
        "beginTransactionNonExclusive", "beginTransactionWithListenerNonExclusive" -> {
            action = DatabaseTransactionAction.BEGIN
            mode = DatabaseTransactionModeKind.IMMEDIATE
        }
        "setTransactionSuccessful" -> {
            action = DatabaseTransactionAction.MARK_SUCCESSFUL
            mode = DatabaseTransactionModeKind.UNKNOWN
        }
        "endTransaction" -> {
            action = DatabaseTransactionAction.END
            mode = DatabaseTransactionModeKind.UNKNOWN
        }
        else -> return null
    }
    return HookIntent.DatabaseTransaction(framework, action, mode)
}

private fun databaseFramework(call: MethodCall): DatabaseFrameworkKind? {
    return when {
        call.matchesOwner(DATABASE_ROOM_OWNERS) -> DatabaseFrameworkKind.ROOM
        call.matchesOwner(DATABASE_SUPPORT_SQLITE_OWNERS) -> DatabaseFrameworkKind.SUPPORT_SQLITE
        call.matchesOwner(DATABASE_PLATFORM_SQLITE_OWNERS) -> DatabaseFrameworkKind.SQLITE
        else -> null
    }
}

private fun databaseQueryArgumentIndex(
    call: MethodCall,
    framework: DatabaseFrameworkKind,
): Int? {
    val owner = when (framework) {
        DatabaseFrameworkKind.SQLITE -> "android/database/sqlite/SQLiteDatabase"
        DatabaseFrameworkKind.SUPPORT_SQLITE -> "androidx/sqlite/db/SupportSQLiteDatabase"
        DatabaseFrameworkKind.ROOM -> "androidx/room/RoomDatabase"
    }
    return databaseQueryArgumentIndex(owner, call.name, call.descriptor)
}

internal fun databaseQueryArgumentIndex(owner: String, name: String, descriptor: String): Int? {
    val candidate = when (owner) {
        "android/database/sqlite/SQLiteDatabase" -> when (name) {
            "rawQuery", "execSQL", "compileStatement" -> 0
            "rawQueryWithFactory" -> 1
            else -> return null
        }
        "androidx/sqlite/db/SupportSQLiteDatabase",
        "androidx/room/RoomDatabase",
        -> when (name) {
            "query", "execSQL", "compileStatement" -> 0
            else -> return null
        }
        else -> return null
    }
    val arguments = Type.getArgumentTypes(descriptor)
    return candidate.takeIf { index ->
        index < arguments.size && arguments[index].sort == Type.OBJECT &&
            arguments[index].internalName == "java/lang/String"
    }
}

internal fun isDatabaseImplementationClass(className: String): Boolean {
    val normalized = className.replace('/', '.')
    return DATABASE_IMPLEMENTATION_PACKAGES.any { packageName ->
        normalized == packageName ||
            normalized.length > packageName.length && normalized.startsWith(packageName) &&
            normalized[packageName.length] == '.'
    }
}

private val DATABASE_IMPLEMENTATION_PACKAGES = arrayOf(
    // Generated DAO implementations live in the application's package and remain observable.
    // These packages contain delegated infrastructure where the same physical query is repeated.
    "androidx.room",
    "androidx.sqlite",
)

private val DATABASE_PLATFORM_SQLITE_OWNERS = setOf(
    "android/database/sqlite/SQLiteDatabase",
    "android/database/sqlite/SQLiteStatement",
)

private val DATABASE_SUPPORT_SQLITE_OWNERS = setOf(
    "androidx/sqlite/db/SupportSQLiteDatabase",
    "androidx/sqlite/db/SupportSQLiteStatement",
)

private val DATABASE_ROOM_OWNERS = setOf(
    "androidx/room/RoomDatabase",
    "androidx/room/EntityInsertAdapter",
    "androidx/room/EntityUpsertAdapter",
    "androidx/room/EntityDeleteOrUpdateAdapter",
)

private val DATABASE_ROOM_INSERT_OWNERS = setOf("androidx/room/EntityInsertAdapter")
private val DATABASE_ROOM_UPSERT_OWNERS = setOf("androidx/room/EntityUpsertAdapter")
private val DATABASE_ROOM_MUTATION_OWNERS = setOf("androidx/room/EntityDeleteOrUpdateAdapter")

private val DATABASE_STATEMENT_OWNERS = setOf(
    "android/database/sqlite/SQLiteStatement",
    "androidx/sqlite/db/SupportSQLiteStatement",
)

private val DATABASE_TRANSACTION_OWNERS = setOf(
    "android/database/sqlite/SQLiteDatabase",
    "androidx/sqlite/db/SupportSQLiteDatabase",
    "androidx/room/RoomDatabase",
)
