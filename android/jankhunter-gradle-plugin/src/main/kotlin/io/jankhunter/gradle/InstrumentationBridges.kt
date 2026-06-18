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
        return call.owner in owners &&
            call.name in names &&
            descriptorMatcher(call)
    }

    companion object {
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

    fun match(call: MethodCall): VersionedBridgeMatch? {
        val signature = signatures.firstOrNull { it.matches(call) } ?: return null
        return VersionedBridgeMatch(id, signature)
    }
}

internal object VersionedBridgeCatalog {
    val okHttp: List<VersionedInstrumentationBridge> = listOf(OkHttp3Bridge)
    val coroutines: List<VersionedInstrumentationBridge> = listOf(KotlinxCoroutinesBridge)

    fun matchOkHttp(call: MethodCall, intents: Set<String>): VersionedBridgeMatch? {
        return okHttp.firstNotNullOfOrNull { bridge ->
            bridge.match(call)?.takeIf { it.intent.id in intents }
        }
    }

    fun matchCoroutine(call: MethodCall): VersionedBridgeMatch? {
        return coroutines.firstNotNullOfOrNull { it.match(call) }
    }

    fun all(): List<VersionedInstrumentationBridge> = okHttp + coroutines
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
