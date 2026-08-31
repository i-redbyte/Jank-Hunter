package io.jankhunter.runtime

import android.view.View
import java.util.concurrent.Callable
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeDecoratorFactoryTest {
    private val callbacks: RuntimeAsyncCallbacks
        get() = JankHunter.asyncTelemetry()
    private val handlerOwner = HandlerRunnableOwner { _, _ -> }

    @Test
    fun publicDecoratorsAreNoopsWhenRuntimeInactive() {
        val runnable = Runnable {}
        val callable = Callable { "ok" }
        val block: Function2<Any?, Any?, Any?> = { _, _ -> "ok" }
        val listener = View.OnClickListener {}

        assertSame(runnable, wrapRunnableDecorator(runnable, "owner", false, callbacks))
        assertSame(callable, wrapCallableDecorator(callable, "owner", false, callbacks))
        assertSame(block, wrapCoroutineBlockDecorator(block, "owner", false, callbacks))
        assertSame(listener, wrapClickListenerDecorator(listener, "owner", false, callbacks))
    }

    @Test
    fun publicDecoratorsAreIdempotent() {
        val runnable = wrapRunnableDecorator(Runnable {}, "owner", true, callbacks)
        val callable = wrapCallableDecorator(Callable { "ok" }, "owner", true, callbacks)
        val blockDelegate: Function2<Any?, Any?, Any?> = { _, _ -> "ok" }
        val block = wrapCoroutineBlockDecorator(blockDelegate, "owner", true, callbacks)
        val listener = wrapClickListenerDecorator(View.OnClickListener {}, "owner", true, callbacks)

        assertSame(runnable, wrapRunnableDecorator(runnable, "owner", true, callbacks))
        assertSame(callable, wrapCallableDecorator(callable, "owner", true, callbacks))
        assertSame(block, wrapCoroutineBlockDecorator(block, "owner", true, callbacks))
        assertSame(listener, wrapClickListenerDecorator(listener, "owner", true, callbacks))
    }

    @Test
    fun publicRunnableAndCallableKeepAdditionalTypeContracts() {
        val priorityRunnable = object : PriorityRunnable {
            override fun run() = Unit
        }
        val priorityCallable = object : PriorityCallable<String> {
            override fun call(): String = "ok"
        }

        assertSame(priorityRunnable, wrapRunnableDecorator(priorityRunnable, "owner", true, callbacks))
        assertSame(priorityCallable, wrapCallableDecorator(priorityCallable, "owner", true, callbacks))
    }

    @Test
    fun handlerRunnableWrapperUsesReplacementDecorator() {
        val runnable = Runnable {}
        val wrapped = wrapHandlerRunnableDecorator(runnable, "owner", true, callbacks, handlerOwner)

        assertNotSame(runnable, wrapped)
        assertTrue(wrapped is JankHunterHandlerRunnable)
        assertSame(wrapped, wrapHandlerRunnableDecorator(wrapped, "owner", true, callbacks, handlerOwner))
    }

    private interface PriorityRunnable : Runnable

    private interface PriorityCallable<T> : Callable<T>
}
