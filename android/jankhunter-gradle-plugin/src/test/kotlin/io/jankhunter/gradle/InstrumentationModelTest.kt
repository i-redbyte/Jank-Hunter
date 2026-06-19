package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationModelTest {
    @Test
    fun signatureSpecMatchesOnlyKnownVariants() {
        val call = methodCall(
            owner = "okhttp3/OkHttpClient\$Builder",
            name = "eventListenerFactory",
            descriptor = "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
        )

        assertTrue(HookSignatureCatalog.okHttpEventListenerFactory.matches(call))
        assertFalse(HookSignatureCatalog.okHttpBuild.matches(call))
        assertEquals(0, HookSignatureCatalog.okHttpEventListenerFactory.roles[ArgumentRole.Listener])
    }

    @Test
    fun resolverMapsExistingHooksToCanonicalIntents() {
        val config = testHookConfig(
            okhttp = true,
            webSockets = true,
            handlers = true,
            executors = true,
            coroutines = true,
            logSpam = true,
        )

        assertIntent(
            HookIntent.WrapOkHttpEventListenerFactory,
            methodCall(
                owner = "okhttp3/OkHttpClient\$Builder",
                name = "eventListenerFactory",
                descriptor = "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
            ),
            config,
        )
        assertIntent(
            HookIntent.WrapWebSocketListener,
            methodCall(
                owner = "okhttp3/OkHttpClient",
                name = "newWebSocket",
                descriptor = "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
            ),
            config,
        )
        assertIntent(
            HookIntent.HandlerRunnable(HandlerRunnableKind.RUNNABLE_LONG_DELAY),
            methodCall(
                owner = "android/os/Handler",
                name = "postDelayed",
                descriptor = "(Ljava/lang/Runnable;J)Z",
            ),
            config,
        )
        assertIntent(
            HookIntent.ExecutorCallable(ExecutorCallableKind.SINGLE_CALLABLE),
            methodCall(
                owner = "java/util/concurrent/ExecutorService",
                name = "submit",
                descriptor = "(Ljava/util/concurrent/Callable;)Ljava/util/concurrent/Future;",
            ),
            config,
        )
        assertIntent(
            HookIntent.CoroutineBlock(CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION),
            methodCall(
                owner = "kotlinx/coroutines/BuildersKt",
                name = "withContext",
                descriptor = "(Lkotlin/coroutines/CoroutineContext;Lkotlin/jvm/functions/Function2;" +
                    "Lkotlin/coroutines/Continuation;)Ljava/lang/Object;",
            ),
            config,
        )
        assertIntent(
            HookIntent.LogSpam("android.util.Log.d", 3),
            methodCall(
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/String;Ljava/lang/String;)I",
            ),
            config,
        )
    }

    @Test
    fun resolverReportsVersionedBridgeForAllHookFamilies() {
        val okHttpDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "okhttp3/OkHttpClient\$Builder",
                name = "build",
                descriptor = "()Lokhttp3/OkHttpClient;",
            ),
            testHookConfig(okhttp = true),
        )
        require(okHttpDecision is HookDecision.Matched)
        assertEquals("okhttp3.bridge.v3", okHttpDecision.bridgeId)
        assertEquals("okhttp3.builder.build.v3", okHttpDecision.signatureId)

        val handlerDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/os/Handler",
                name = "post",
                descriptor = "(Ljava/lang/Runnable;)Z",
            ),
            testHookConfig(handlers = true),
        )
        require(handlerDecision is HookDecision.Matched)
        assertEquals("android.handler.bridge.v1", handlerDecision.bridgeId)

        val executorDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "java/util/concurrent/Executor",
                name = "execute",
                descriptor = "(Ljava/lang/Runnable;)V",
            ),
            testHookConfig(executors = true),
        )
        require(executorDecision is HookDecision.Matched)
        assertEquals("jdk.executor.bridge.v1", executorDecision.bridgeId)

        val coroutineDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "kotlinx/coroutines/BuildersKt",
                name = "launch\$default",
                descriptor = "(Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                    "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;ILjava/lang/Object;)" +
                    "Lkotlinx/coroutines/Job;",
            ),
            testHookConfig(coroutines = true),
        )
        require(coroutineDecision is HookDecision.Matched)
        assertEquals("kotlinx.coroutines.bridge.v1", coroutineDecision.bridgeId)
        assertEquals("kotlinx.coroutines.builders.default_function2.v1", coroutineDecision.signatureId)

        val flowDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/view/View",
                name = "setOnClickListener",
                descriptor = "(Landroid/view/View\$OnClickListener;)V",
            ),
            testHookConfig(flowInteractions = true),
        )
        require(flowDecision is HookDecision.Matched)
        assertEquals("android.view.flow.bridge.v1", flowDecision.bridgeId)

        val logSpamDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/String;Ljava/lang/String;)I",
            ),
            testHookConfig(logSpam = true),
        )
        require(logSpamDecision is HookDecision.Matched)
        assertEquals("android.log.bridge.v1", logSpamDecision.bridgeId)
    }

    @Test
    fun moduleRegistryExposesExtensibleFamilies() {
        val bridgeIds = VersionedBridgeCatalog.all().map { it.id }.toSet()
        val families = VersionedBridgeCatalog.all().map { it.family }.toSet()
        val moduleIds = HookIntentResolver.modules().map { it.id }.toSet()
        val providerBridgeIds = DefaultInstrumentationBridgeProvider.bridges().map { it.id }.toSet()

        assertTrue("okhttp3.bridge.v3" in bridgeIds)
        assertTrue("android.handler.bridge.v1" in bridgeIds)
        assertTrue("jdk.executor.bridge.v1" in bridgeIds)
        assertTrue("kotlinx.coroutines.bridge.v1" in bridgeIds)
        assertTrue("android.view.flow.bridge.v1" in bridgeIds)
        assertTrue("android.log.bridge.v1" in bridgeIds)
        assertEquals(providerBridgeIds, bridgeIds)
        assertTrue("okhttp" in families)
        assertTrue("handler" in families)
        assertTrue("executor" in families)
        assertTrue("coroutines" in families)
        assertTrue("flow" in families)
        assertTrue("logspam" in families)
        assertEquals(
            setOf("okhttp", "websocket", "handler", "executor", "coroutine", "flow", "logspam"),
            moduleIds,
        )
        assertTrue(
            VersionedBridgeCatalog.all()
                .flatMap { it.signatures }
                .any { it.id == "kotlinx.coroutines.suspend_builders.function2_continuation.v1" },
        )
    }

    @Test
    fun currentInstrumentationModulesStayLinearByDefault() {
        assertFalse(HookIntentResolver.needsControlFlow())
        assertFalse(HookIntentResolver.needsControlFlow(testHookConfig(okhttp = true, coroutines = true)))
        assertTrue(HookIntentResolver.modules().none { it.needsControlFlow })
    }

    @Test
    fun ownerLabelsAreStableAndReadable() {
        val first = OwnerIds.ownerLabel("com/example/Foo", "load", "()V")
        val second = OwnerIds.ownerLabel("com/example/Foo", "load", "()V")
        val differentDescriptor = OwnerIds.ownerLabel("com/example/Foo", "load", "(I)V")

        assertEquals(first, second)
        assertTrue(first.startsWith("com.example.Foo.load#"))
        assertFalse(first == differentDescriptor)
    }

    @Test
    fun resolverRespectsDisabledGates() {
        val call = methodCall(
            owner = "android/os/Handler",
            name = "post",
            descriptor = "(Ljava/lang/Runnable;)Z",
        )

        val decision = HookIntentResolver.resolve(call, testHookConfig())
        require(decision is HookDecision.Disabled)
        assertEquals("handler", decision.moduleId)
        assertEquals("disabled_by_gate", decision.reason)
    }

    @Test
    fun resolverKeepsWebSocketGateSeparateFromOkHttpGate() {
        val call = methodCall(
            owner = "okhttp3/OkHttpClient",
            name = "newWebSocket",
            descriptor = "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
        )

        val disabled = HookIntentResolver.resolve(call, testHookConfig(okhttp = true))
        require(disabled is HookDecision.Disabled)
        assertEquals("websocket", disabled.moduleId)
        assertIntent(HookIntent.WrapWebSocketListener, call, testHookConfig(webSockets = true))
    }

    @Test
    fun resolverReportsUnsupportedKnownOwnerSignature() {
        val decision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/os/Handler",
                name = "post",
                descriptor = "(Ljava/lang/Runnable;Ljava/lang/Object;)Z",
            ),
            testHookConfig(handlers = true),
        )

        require(decision is HookDecision.Unsupported)
        assertEquals("handler", decision.moduleId)
        assertEquals("unsupported_signature", decision.reason)
    }

    @Test
    fun logSpamBridgeRejectsUnknownDescriptors() {
        val supported = HookIntentResolver.resolve(
            methodCall(
                owner = "android/util/Log",
                name = "w",
                descriptor = "(Ljava/lang/String;Ljava/lang/Throwable;)I",
            ),
            testHookConfig(logSpam = true),
        )
        require(supported is HookDecision.Matched)
        assertEquals("android.log.bridge.v1", supported.bridgeId)

        val unsupported = HookIntentResolver.resolve(
            methodCall(
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/Object;)V",
            ),
            testHookConfig(logSpam = true),
        )
        require(unsupported is HookDecision.Unsupported)
        assertEquals("logspam", unsupported.moduleId)
    }

    private fun assertIntent(expected: HookIntent, call: MethodCall, config: HookConfig) {
        val decision = HookIntentResolver.resolve(call, config)
        require(decision is HookDecision.Matched) { "Expected matched hook for $call but got $decision" }
        assertEquals(expected, decision.intent)
        assertTrue(decision.signatureId.isNotBlank())
    }

    private fun methodCall(owner: String, name: String, descriptor: String): MethodCall {
        return MethodCall(owner = owner, name = name, descriptor = descriptor)
    }

    private fun testHookConfig(
        methodCounters: Boolean = false,
        okhttp: Boolean = false,
        webSockets: Boolean = false,
        handlers: Boolean = false,
        executors: Boolean = false,
        coroutines: Boolean = false,
        flowInteractions: Boolean = false,
        logSpam: Boolean = false,
        classGraph: Boolean = false,
        runtimeCallGraph: Boolean = false,
    ): HookConfig {
        return HookConfig(
            methodCounters = methodCounters,
            okhttp = okhttp,
            webSockets = webSockets,
            handlers = handlers,
            executors = executors,
            coroutines = coroutines,
            flowInteractions = flowInteractions,
            logSpam = logSpam,
            classGraph = classGraph,
            runtimeCallGraph = runtimeCallGraph,
            classGraphDirectory = "",
            instrumentationDiagnosticsDirectory = "",
            ownerMapEntriesDirectory = "",
        )
    }
}
