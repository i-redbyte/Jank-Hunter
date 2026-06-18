package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

class InstrumentationModelTest {
    @Test
    fun descriptorIteratorTracksWideSlots() {
        val args = DescriptorArgumentIterator(
            "(ILjava/lang/String;JDLjava/lang/Object;[I)V",
            startSlot = 1,
        ).toList()

        assertEquals(6, args.size)
        assertEquals(0, args[0].index)
        assertEquals(Type.INT_TYPE, args[0].type)
        assertEquals(1, args[0].slotIndex)
        assertEquals(2, args[1].slotIndex)
        assertEquals(3, args[2].slotIndex)
        assertEquals(Type.LONG_TYPE, args[2].type)
        assertEquals(5, args[3].slotIndex)
        assertEquals(Type.DOUBLE_TYPE, args[3].type)
        assertEquals(7, args[4].slotIndex)
        assertEquals(8, args[5].slotIndex)
    }

    @Test
    fun methodCallNormalizesOpcodeSourceBits() {
        val call = MethodCall(
            opcode = Opcodes.INVOKEVIRTUAL or Opcodes.SOURCE_DEPRECATED,
            owner = "example/Foo",
            name = "bar",
            descriptor = "()V",
            isInterface = false,
        )

        assertEquals(InvocationKind.Virtual, call.invocationKind)
    }

    @Test
    fun signatureSpecMatchesOnlyKnownVariants() {
        val call = MethodCall(
            opcode = Opcodes.INVOKEVIRTUAL,
            owner = "okhttp3/OkHttpClient\$Builder",
            name = "eventListenerFactory",
            descriptor = "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
            isInterface = false,
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
            MethodCall(
                opcode = Opcodes.INVOKEVIRTUAL,
                owner = "okhttp3/OkHttpClient\$Builder",
                name = "eventListenerFactory",
                descriptor = "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
                isInterface = false,
            ),
            config,
        )
        assertIntent(
            HookIntent.WrapWebSocketListener,
            MethodCall(
                opcode = Opcodes.INVOKEVIRTUAL,
                owner = "okhttp3/OkHttpClient",
                name = "newWebSocket",
                descriptor = "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
                isInterface = false,
            ),
            config,
        )
        assertIntent(
            HookIntent.HandlerRunnable(HandlerRunnableKind.RUNNABLE_LONG_DELAY),
            MethodCall(
                opcode = Opcodes.INVOKEVIRTUAL,
                owner = "android/os/Handler",
                name = "postDelayed",
                descriptor = "(Ljava/lang/Runnable;J)Z",
                isInterface = false,
            ),
            config,
        )
        assertIntent(
            HookIntent.ExecutorCallable(ExecutorCallableKind.SINGLE_CALLABLE),
            MethodCall(
                opcode = Opcodes.INVOKEINTERFACE,
                owner = "java/util/concurrent/ExecutorService",
                name = "submit",
                descriptor = "(Ljava/util/concurrent/Callable;)Ljava/util/concurrent/Future;",
                isInterface = true,
            ),
            config,
        )
        assertIntent(
            HookIntent.CoroutineBlock(CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION),
            MethodCall(
                opcode = Opcodes.INVOKESTATIC,
                owner = "kotlinx/coroutines/BuildersKt",
                name = "withContext",
                descriptor = "(Lkotlin/coroutines/CoroutineContext;Lkotlin/jvm/functions/Function2;" +
                    "Lkotlin/coroutines/Continuation;)Ljava/lang/Object;",
                isInterface = false,
            ),
            config,
        )
        assertIntent(
            HookIntent.LogSpam("android.util.Log.d", 3),
            MethodCall(
                opcode = Opcodes.INVOKESTATIC,
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/String;Ljava/lang/String;)I",
                isInterface = false,
            ),
            config,
        )
    }

    @Test
    fun resolverReportsVersionedBridgeForAllHookFamilies() {
        val okHttpDecision = HookIntentResolver.resolve(
            MethodCall(
                opcode = Opcodes.INVOKEVIRTUAL,
                owner = "okhttp3/OkHttpClient\$Builder",
                name = "build",
                descriptor = "()Lokhttp3/OkHttpClient;",
                isInterface = false,
            ),
            testHookConfig(okhttp = true),
        )
        require(okHttpDecision is HookDecision.Matched)
        assertEquals("okhttp3.bridge.v3", okHttpDecision.bridgeId)
        assertEquals("okhttp3.builder.build.v3", okHttpDecision.signatureId)

        val handlerDecision = HookIntentResolver.resolve(
            MethodCall(
                opcode = Opcodes.INVOKEVIRTUAL,
                owner = "android/os/Handler",
                name = "post",
                descriptor = "(Ljava/lang/Runnable;)Z",
                isInterface = false,
            ),
            testHookConfig(handlers = true),
        )
        require(handlerDecision is HookDecision.Matched)
        assertEquals("android.handler.bridge.v1", handlerDecision.bridgeId)

        val executorDecision = HookIntentResolver.resolve(
            MethodCall(
                opcode = Opcodes.INVOKEINTERFACE,
                owner = "java/util/concurrent/Executor",
                name = "execute",
                descriptor = "(Ljava/lang/Runnable;)V",
                isInterface = true,
            ),
            testHookConfig(executors = true),
        )
        require(executorDecision is HookDecision.Matched)
        assertEquals("jdk.executor.bridge.v1", executorDecision.bridgeId)

        val coroutineDecision = HookIntentResolver.resolve(
            MethodCall(
                opcode = Opcodes.INVOKESTATIC,
                owner = "kotlinx/coroutines/BuildersKt",
                name = "launch\$default",
                descriptor = "(Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                    "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;ILjava/lang/Object;)" +
                    "Lkotlinx/coroutines/Job;",
                isInterface = false,
            ),
            testHookConfig(coroutines = true),
        )
        require(coroutineDecision is HookDecision.Matched)
        assertEquals("kotlinx.coroutines.bridge.v1", coroutineDecision.bridgeId)
        assertEquals("kotlinx.coroutines.builders.default_function2.v1", coroutineDecision.signatureId)

        val flowDecision = HookIntentResolver.resolve(
            MethodCall(
                opcode = Opcodes.INVOKEVIRTUAL,
                owner = "android/view/View",
                name = "setOnClickListener",
                descriptor = "(Landroid/view/View\$OnClickListener;)V",
                isInterface = false,
            ),
            testHookConfig(flowInteractions = true),
        )
        require(flowDecision is HookDecision.Matched)
        assertEquals("android.view.flow.bridge.v1", flowDecision.bridgeId)

        val logSpamDecision = HookIntentResolver.resolve(
            MethodCall(
                opcode = Opcodes.INVOKESTATIC,
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/String;Ljava/lang/String;)I",
                isInterface = false,
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

        assertTrue("okhttp3.bridge.v3" in bridgeIds)
        assertTrue("android.handler.bridge.v1" in bridgeIds)
        assertTrue("jdk.executor.bridge.v1" in bridgeIds)
        assertTrue("kotlinx.coroutines.bridge.v1" in bridgeIds)
        assertTrue("android.view.flow.bridge.v1" in bridgeIds)
        assertTrue("android.log.bridge.v1" in bridgeIds)
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
    fun resolverRespectsDisabledGates() {
        val call = MethodCall(
            opcode = Opcodes.INVOKEVIRTUAL,
            owner = "android/os/Handler",
            name = "post",
            descriptor = "(Ljava/lang/Runnable;)Z",
            isInterface = false,
        )

        assertEquals(HookDecision.NotMatched, HookIntentResolver.resolve(call, testHookConfig()))
    }

    @Test
    fun resolverKeepsWebSocketGateSeparateFromOkHttpGate() {
        val call = MethodCall(
            opcode = Opcodes.INVOKEVIRTUAL,
            owner = "okhttp3/OkHttpClient",
            name = "newWebSocket",
            descriptor = "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
            isInterface = false,
        )

        assertEquals(HookDecision.NotMatched, HookIntentResolver.resolve(call, testHookConfig(okhttp = true)))
        assertIntent(HookIntent.WrapWebSocketListener, call, testHookConfig(webSockets = true))
    }

    private fun assertIntent(expected: HookIntent, call: MethodCall, config: HookConfig) {
        val decision = HookIntentResolver.resolve(call, config)
        require(decision is HookDecision.Matched) { "Expected matched hook for $call but got $decision" }
        assertEquals(expected, decision.intent)
        assertTrue(decision.signatureId.isNotBlank())
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
            classGraphPath = "",
            instrumentationDiagnosticsPath = "",
        )
    }
}
