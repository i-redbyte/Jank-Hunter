package io.jankhunter.runtime

import android.view.View
import java.util.concurrent.Callable
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeDecoratorFactoryTest {
    @Test
    fun publicDecoratorsAreNoopsWhenRuntimeInactive() {
        val runnable = Runnable {}
        val callable = Callable { "ok" }
        val block: Function2<Any?, Any?, Any?> = { _, _ -> "ok" }
        val listener = View.OnClickListener {}

        assertSame(runnable, RuntimeDecoratorFactory.wrapRunnable(runnable, "owner", runtimeActive = false))
        assertSame(callable, RuntimeDecoratorFactory.wrapCallable(callable, "owner", runtimeActive = false))
        assertSame(block, RuntimeDecoratorFactory.wrapCoroutineBlock(block, "owner", runtimeActive = false))
        assertSame(listener, RuntimeDecoratorFactory.wrapClickListener(listener, "owner", runtimeActive = false))
    }

    @Test
    fun publicDecoratorsAreIdempotent() {
        val runnable = RuntimeDecoratorFactory.wrapRunnable(Runnable {}, "owner", runtimeActive = true)
        val callable = RuntimeDecoratorFactory.wrapCallable(Callable { "ok" }, "owner", runtimeActive = true)
        val blockDelegate: Function2<Any?, Any?, Any?> = { _, _ -> "ok" }
        val block = RuntimeDecoratorFactory.wrapCoroutineBlock(blockDelegate, "owner", runtimeActive = true)
        val listener = RuntimeDecoratorFactory.wrapClickListener(View.OnClickListener {}, "owner", runtimeActive = true)

        assertSame(runnable, RuntimeDecoratorFactory.wrapRunnable(runnable, "owner", runtimeActive = true))
        assertSame(callable, RuntimeDecoratorFactory.wrapCallable(callable, "owner", runtimeActive = true))
        assertSame(block, RuntimeDecoratorFactory.wrapCoroutineBlock(block, "owner", runtimeActive = true))
        assertSame(listener, RuntimeDecoratorFactory.wrapClickListener(listener, "owner", runtimeActive = true))
    }

    @Test
    fun publicRunnableAndCallableKeepAdditionalTypeContracts() {
        val priorityRunnable = object : PriorityRunnable {
            override fun run() = Unit
        }
        val priorityCallable = object : PriorityCallable<String> {
            override fun call(): String = "ok"
        }

        assertSame(priorityRunnable, RuntimeDecoratorFactory.wrapRunnable(priorityRunnable, "owner", runtimeActive = true))
        assertSame(priorityCallable, RuntimeDecoratorFactory.wrapCallable(priorityCallable, "owner", runtimeActive = true))
    }

    @Test
    fun handlerRunnableWrapperUsesReplacementDecorator() {
        val runnable = Runnable {}
        val wrapped = RuntimeDecoratorFactory.wrapHandlerRunnable(runnable, "owner", runtimeActive = true)

        assertNotSame(runnable, wrapped)
        assertTrue(wrapped is JankHunterHandlerRunnable)
        assertSame(wrapped, RuntimeDecoratorFactory.wrapHandlerRunnable(wrapped, "owner", runtimeActive = true))
    }

    private interface PriorityRunnable : Runnable

    private interface PriorityCallable<T> : Callable<T>
}
