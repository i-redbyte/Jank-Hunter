package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import android.os.Handler
import android.view.View
import java.io.File
import java.lang.reflect.Modifier
import java.nio.file.Files
import java.util.concurrent.Callable
import java.util.UUID
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.After
import org.junit.Test

class JankHunterHooksTest {
    @After
    fun tearDown() {
        JankHunter.shutdown()
    }

    @Test
    fun facadeHasStableJvmOwnerAndStaticEntrypoints() {
        val facade = Class.forName("io.jankhunter.runtime.JankHunterHooks")

        assertEquals(listOf("INSTANCE"), facade.declaredFields.map { it.name }.sorted())
        val signatures = listOf(
            "enterMethod" to arrayOf(java.lang.Long.TYPE, String::class.java),
            "exitMethod" to arrayOf(java.lang.Long.TYPE, java.lang.Long.TYPE),
            "recordMethodCall" to arrayOf(java.lang.Long.TYPE, String::class.java),
            "recordCounter" to arrayOf(String::class.java, java.lang.Long.TYPE),
            "recordLogSpam" to arrayOf(String::class.java, String::class.java, Integer.TYPE),
            "workerInstanceId" to arrayOf(Any::class.java),
            "enterWorker" to arrayOf(
                java.lang.Long.TYPE,
                java.lang.Long.TYPE,
                String::class.java,
                Integer.TYPE,
            ),
            "exitWorker" to arrayOf(
                java.lang.Long.TYPE,
                java.lang.Long.TYPE,
                java.lang.Long.TYPE,
                String::class.java,
                Integer.TYPE,
                Integer.TYPE,
            ),
            "wrapRunnable" to arrayOf(Runnable::class.java, String::class.java),
            "wrapCallable" to arrayOf(Callable::class.java, String::class.java),
            "wrapCoroutineBlock" to arrayOf(Function2::class.java, String::class.java),
            "wrapClickListener" to arrayOf(View.OnClickListener::class.java, String::class.java),
            "wrapHandlerRunnable" to arrayOf(
                Handler::class.java,
                Runnable::class.java,
                Any::class.java,
                String::class.java,
            ),
            "onHandlerPostResult" to arrayOf(Runnable::class.java, Runnable::class.java, java.lang.Boolean.TYPE),
            "handlerWrappers" to arrayOf(Handler::class.java, Runnable::class.java, Any::class.java),
            "clearHandlerWrappers" to arrayOf(
                Handler::class.java,
                Runnable::class.java,
                Any::class.java,
            ),
            "clearHandlerWrappers" to arrayOf(Handler::class.java, Any::class.java),
            "enterAnnotatedContext" to arrayOf(
                String::class.java,
                String::class.java,
            ),
            "exitAnnotatedContext" to arrayOf(Any::class.java),
            "startAnnotatedOperation" to arrayOf(String::class.java, Integer.TYPE, java.lang.Long.TYPE),
            "finishAnnotatedOperation" to arrayOf(Any::class.java, java.lang.Boolean.TYPE),
            "watchLifecycleObject" to arrayOf(Any::class.java, String::class.java, String::class.java),
        )
        signatures.forEach { (name, parameterTypes) ->
            assertTrue(
                "$name must remain static",
                Modifier.isStatic(facade.getDeclaredMethod(name, *parameterTypes).modifiers),
            )
        }
    }

    @Test
    fun inactiveFacadeReturnsIdentityWithoutInvokingBusinessWork() {
        JankHunter.shutdown()
        val calls = AtomicInteger()
        val runnable = Runnable { calls.incrementAndGet() }
        val callable = Callable { calls.incrementAndGet() }

        val wrappedRunnable = JankHunterHooks.wrapRunnable(runnable, "owner")
        val wrappedCallable = JankHunterHooks.wrapCallable(callable, "owner")

        assertSame(runnable, wrappedRunnable)
        assertSame(callable, wrappedCallable)
        assertEquals(0, calls.get())
        assertEquals(0L, JankHunterHooks.enterMethod(0L, "test.Owner.call"))
        assertEquals(0L, JankHunterHooks.workerInstanceId(UUID(1L, 2L)))
        JankHunterHooks.recordMethodCall(0L, "test.Owner.call")
        JankHunterHooks.exitMethod(0L, 0L)
        assertEquals(0, calls.get())
    }

    @Test
    fun disabledBytecodeFeaturesReturnOriginalObjectsWithoutWrapperAllocations() {
        val directory = Files.createTempDirectory("jankhunter-disabled-hooks").toFile()
        val context = TestContext(directory)
        val runnable = Runnable {}
        val callable = Callable { Unit }
        val coroutine: (Any?, Any?) -> Any? = { _, _ -> Unit }
        val clickListener = View.OnClickListener {}
        JankHunter.init(
            context,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .logDirectory(directory)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, false)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.COROUTINES, false)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.INTERACTIONS, false)
                .build(),
        )

        assertSame(runnable, JankHunterHooks.wrapRunnable(runnable, "owner"))
        assertSame(callable, JankHunterHooks.wrapCallable(callable, "owner"))
        assertSame(coroutine, JankHunterHooks.wrapCoroutineBlock(coroutine, "owner"))
        assertSame(clickListener, JankHunterHooks.wrapClickListener(clickListener, "owner"))
    }

    @Test
    fun networkRuntimeExposesIndependentHttpAndWebSocketGates() {
        val directory = Files.createTempDirectory("jankhunter-disabled-network").toFile()
        JankHunter.init(
            TestContext(directory),
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .logDirectory(directory)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.HTTP, false)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.WEBSOCKETS, true)
                .build(),
        )

        assertFalse(JankHunterNetworkRuntime.isHttpActive())
        assertTrue(JankHunterNetworkRuntime.isWebSocketActive())
    }

    @Test
    fun exitUsesEnterTokenAfterFeatureIsDisabled() {
        val directory = Files.createTempDirectory("jankhunter-hook-token").toFile()
        JankHunter.init(
            TestContext(directory),
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .logDirectory(directory)
                .build(),
        )
        val token = JankHunterHooks.enterAnnotatedContext("Checkout", "submit")
        assertEquals("Checkout", JankHunterTelemetry.currentScreen())

        assertTrue(JankHunter.reconfigure("remote_config") { builder ->
            builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.INTERACTIONS, false)
        })
        JankHunterHooks.exitAnnotatedContext(token)

        assertEquals("unknown", JankHunterTelemetry.currentScreen())
        assertEquals("unknown", JankHunterTelemetry.currentOwner())
    }

    private class TestContext(private val filesDirectory: File) : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this

        override fun getPackageName(): String = "com.example"

        override fun getFilesDir(): File = filesDirectory

        override fun getSystemService(name: String): Any? = null
    }
}
