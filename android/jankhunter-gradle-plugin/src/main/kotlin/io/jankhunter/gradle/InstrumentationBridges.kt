package io.jankhunter.gradle

internal data class VersionedBridgeSignature(
    val id: String,
    val intent: HookIntent,
    val owners: Set<String>,
    val names: Set<String>,
    val roles: Map<ArgumentRole, Int> = emptyMap(),
    private val descriptorMatcher: (MethodCall) -> Boolean,
) {
    fun matches(call: MethodCall): Boolean {
        return call.matchesOwner(owners) &&
            call.name in names &&
            descriptorMatcher(call)
    }

    companion object {
        fun exact(signature: IntentSignature): VersionedBridgeSignature {
            return exact(signature.spec, signature.intent)
        }

        fun exact(spec: SignatureSpec, intent: HookIntent): VersionedBridgeSignature {
            return VersionedBridgeSignature(
                id = spec.id,
                intent = intent,
                owners = spec.owners,
                names = spec.names,
                roles = spec.roles,
                descriptorMatcher = { call -> call.descriptor in spec.descriptors },
            )
        }

        fun covariantObjectReturn(signature: IntentSignature): VersionedBridgeSignature {
            val spec = signature.spec
            val parameterDescriptors = spec.descriptors.mapTo(linkedSetOf()) { descriptor ->
                descriptor.substring(0, descriptor.lastIndexOf(')') + 1)
            }
            return VersionedBridgeSignature(
                id = spec.id,
                intent = signature.intent,
                owners = spec.owners,
                names = spec.names,
                roles = spec.roles,
                descriptorMatcher = { call ->
                    val argumentsEnd = call.descriptor.lastIndexOf(')')
                    argumentsEnd >= 0 &&
                        call.descriptor.substring(0, argumentsEnd + 1) in parameterDescriptors &&
                        call.descriptor.substring(argumentsEnd + 1).let { result ->
                            result.startsWith('L') && result.endsWith(';')
                        }
                },
            )
        }
    }
}

internal data class VersionedBridgeMatch(
    val bridgeId: String,
    val signature: VersionedBridgeSignature,
) {
    val intent: HookIntent
        get() = signature.intent

    fun toDecision(): HookDecision.Matched {
        return HookDecision.Matched(
            intent = signature.intent,
            signatureId = signature.id,
            bridgeId = bridgeId,
        )
    }
}

internal interface VersionedInstrumentationBridge {
    val id: String
    val family: String
    val signatures: List<VersionedBridgeSignature>

    fun relevant(call: MethodCall): Boolean {
        return signatures.any { signature ->
            call.matchesOwner(signature.owners) && call.name in signature.names
        }
    }

    fun match(call: MethodCall): VersionedBridgeMatch? {
        val signature = signatures.firstOrNull { it.matches(call) } ?: return null
        return VersionedBridgeMatch(id, signature)
    }
}

internal fun interface InstrumentationBridgeProvider {
    fun bridges(): List<VersionedInstrumentationBridge>
}

internal object DefaultInstrumentationBridgeProvider : InstrumentationBridgeProvider {
    override fun bridges(): List<VersionedInstrumentationBridge> = listOf(
        OkHttp3Bridge,
        AndroidHandlerBridge,
        JdkExecutorBridge,
        KotlinxCoroutinesBridge,
        AndroidViewFlowBridge,
        AndroidLogSpamBridge,
        TimberLogSpamBridge,
    )
}

internal class InstrumentationBridgeRegistry(
    providers: List<InstrumentationBridgeProvider>,
) {
    private val bridges: List<VersionedInstrumentationBridge> = providers.flatMap { it.bridges() }
    private val byFamily: Map<String, List<VersionedInstrumentationBridge>> = bridges.groupBy { it.family }

    fun family(family: String): List<VersionedInstrumentationBridge> = byFamily[family].orEmpty()

    fun all(): List<VersionedInstrumentationBridge> = bridges
}

internal object VersionedBridgeCatalog {
    private val registry = InstrumentationBridgeRegistry(listOf(DefaultInstrumentationBridgeProvider))
    val okHttp: List<VersionedInstrumentationBridge> = registry.family("okhttp")
    val handlers: List<VersionedInstrumentationBridge> = registry.family("handler")
    val executors: List<VersionedInstrumentationBridge> = registry.family("executor")
    val coroutines: List<VersionedInstrumentationBridge> = registry.family("coroutines")
    val flows: List<VersionedInstrumentationBridge> = registry.family("flow")
    val logSpam: List<VersionedInstrumentationBridge> = registry.family("logspam")

    fun matchOkHttp(call: MethodCall, intents: Set<String>): VersionedBridgeMatch? {
        return okHttp.firstNotNullOfOrNull { bridge ->
            bridge.match(call)?.takeIf { it.intent.id in intents }
        }
    }

    fun all(): List<VersionedInstrumentationBridge> = registry.all()
}

private object OkHttp3Bridge : VersionedInstrumentationBridge {
    override val id: String = "okhttp3.bridge.v3"
    override val family: String = "okhttp"
    override val signatures: List<VersionedBridgeSignature> = listOf(
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpEventListenerFactory,
            HookIntent.WrapOkHttpEventListenerFactory,
        ),
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpBuild,
            HookIntent.InstallOkHttpEventListenerFactory,
        ),
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpNewWebSocket,
            HookIntent.WrapWebSocketListener,
        ),
    )
}

private object KotlinxCoroutinesBridge : VersionedInstrumentationBridge {
    override val id: String = "kotlinx.coroutines.bridge.v1"
    override val family: String = "coroutines"

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

    override val signatures: List<VersionedBridgeSignature> = listOf(
        VersionedBridgeSignature(
            id = "kotlinx.coroutines.builders.top_function2.v1",
            intent = HookIntent.CoroutineBlock(CoroutineBlockKind.TOP_FUNCTION2),
            owners = coroutineBuilderOwners,
            names = coroutineBuildersWithTopBlock,
            descriptorMatcher = { call ->
                call.descriptor.endsWith(
                    "Lkotlin/jvm/functions/Function2;)${returnDescriptor(call.owner, call.name)}",
                )
            },
        ),
        VersionedBridgeSignature(
            id = "kotlinx.coroutines.builders.default_function2.v1",
            intent = HookIntent.CoroutineBlock(CoroutineBlockKind.FUNCTION2_BEFORE_INT_OBJECT),
            owners = coroutineBuilderOwners,
            names = coroutineBuildersWithDefaultBlock,
            descriptorMatcher = { call ->
                call.descriptor.endsWith(defaultCoroutineDescriptorSuffix(call.owner, call.name))
            },
        ),
        VersionedBridgeSignature(
            id = "kotlinx.coroutines.suspend_builders.function2_continuation.v1",
            intent = HookIntent.CoroutineBlock(CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION),
            owners = coroutineBuilderOwners,
            names = coroutineSuspendBuilders,
            descriptorMatcher = { call ->
                call.descriptor.endsWith(SUSPEND_COROUTINE_DESCRIPTOR_SUFFIX)
            },
        ),
    )

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

    private const val SUSPEND_COROUTINE_DESCRIPTOR_SUFFIX =
        "Lkotlin/jvm/functions/Function2;Lkotlin/coroutines/Continuation;)Ljava/lang/Object;"
}

private object AndroidHandlerBridge : VersionedInstrumentationBridge {
    override val id: String = "android.handler.bridge.v1"
    override val family: String = "handler"
    override val signatures: List<VersionedBridgeSignature> =
        HookSignatureCatalog.handlerRunnableSignatures.map(VersionedBridgeSignature::exact) +
            HookSignatureCatalog.handlerRemoveCallbacksSignatures.map(VersionedBridgeSignature::exact) +
            VersionedBridgeSignature.exact(HookSignatureCatalog.handlerRemoveCallbacksAndMessages) +
            VersionedBridgeSignature.exact(HookSignatureCatalog.handlerHasCallbacks) +
            HookSignatureCatalog.handlerMessageSendSignatures.map {
                VersionedBridgeSignature.exact(it, HookIntent.HandlerMessageSend)
            }
}

private object JdkExecutorBridge : VersionedInstrumentationBridge {
    override val id: String = "jdk.executor.bridge.v2"
    override val family: String = "executor"
    override val signatures: List<VersionedBridgeSignature> =
        HookSignatureCatalog.executorRunnableSignatures.map(::executorSignature) +
            HookSignatureCatalog.executorCallableSignatures.map(::executorSignature)

    private fun executorSignature(signature: IntentSignature): VersionedBridgeSignature {
        return if (signature.spec.names == setOf("submit")) {
            // ExecutorService permits covariant Future implementations. Guava's
            // ListeningExecutorService therefore emits ListenableFuture in the call descriptor.
            VersionedBridgeSignature.covariantObjectReturn(signature)
        } else {
            VersionedBridgeSignature.exact(signature)
        }
    }
}

private object AndroidViewFlowBridge : VersionedInstrumentationBridge {
    override val id: String = "android.view.flow.bridge.v1"
    override val family: String = "flow"
    override val signatures: List<VersionedBridgeSignature> = listOf(
        VersionedBridgeSignature(
            id = "android.view.click_listener.v1",
            intent = HookIntent.WrapClickListener,
            owners = setOf("android/view/View"),
            names = setOf("setOnClickListener"),
            roles = mapOf(ArgumentRole.Listener to 0),
            descriptorMatcher = { call ->
                call.descriptor == "(Landroid/view/View\$OnClickListener;)V"
            },
        ),
    )
}

private object AndroidLogSpamBridge : VersionedInstrumentationBridge {
    override val id: String = "android.log.bridge.v1"
    override val family: String = "logspam"
    override val signatures: List<VersionedBridgeSignature> =
        logSpamSignatures(
            owner = "android/util/Log",
            sourcePrefix = "android.util.Log",
            descriptors = androidLogDescriptors,
        )
}

private object TimberLogSpamBridge : VersionedInstrumentationBridge {
    override val id: String = "timber.log.bridge.v1"
    override val family: String = "logspam"
    override val signatures: List<VersionedBridgeSignature> =
        logSpamSignatures(
            owner = "timber/log/Timber",
            sourcePrefix = "Timber",
            descriptors = timberDescriptors,
        ) +
            logSpamSignatures(
                owner = "timber/log/Timber\$Tree",
                sourcePrefix = "Timber.Tree",
                descriptors = timberDescriptors,
            )
}

private fun logSpamSignatures(
    owner: String,
    sourcePrefix: String,
    descriptors: Set<String>,
): List<VersionedBridgeSignature> {
    return listOf(
        "v" to 2,
        "d" to 3,
        "i" to 4,
        "w" to 5,
        "e" to 6,
        "wtf" to 7,
    ).map { (name, level) ->
        val source = "$sourcePrefix.$name"
        VersionedBridgeSignature(
            id = "logspam.$source",
            intent = HookIntent.LogSpam(source, level),
            owners = setOf(owner),
            names = setOf(name),
            descriptorMatcher = { call -> call.descriptor in descriptors },
        )
    }
}

private val androidLogDescriptors = setOf(
    "(Ljava/lang/String;Ljava/lang/String;)I",
    "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/Throwable;)I",
    "(Ljava/lang/String;Ljava/lang/Throwable;)I",
)

private val timberDescriptors = setOf(
    "(Ljava/lang/String;[Ljava/lang/Object;)V",
    "(Ljava/lang/Throwable;Ljava/lang/String;[Ljava/lang/Object;)V",
    "(Ljava/lang/Throwable;)V",
)
