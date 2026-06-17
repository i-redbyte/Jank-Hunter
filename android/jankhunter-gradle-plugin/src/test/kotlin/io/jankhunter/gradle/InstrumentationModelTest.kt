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
        val config = testHookConfig(okhttp = true, handlers = true, executors = true, coroutines = true, logSpam = true)

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
        )
    }
}

