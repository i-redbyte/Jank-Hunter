package io.jankhunter.runtime

import android.os.Handler
import android.view.View
import java.lang.reflect.Modifier
import java.util.concurrent.Callable
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterHooksTest {
    @Test
    fun facadeHasStableJvmOwnerAndStaticEntrypoints() {
        val facade = Class.forName("io.jankhunter.runtime.JankHunterHooks")

        assertEquals(listOf("INSTANCE"), facade.declaredFields.map { it.name }.sorted())
        val signatures = listOf(
            "enterMethod" to arrayOf(java.lang.Long.TYPE),
            "exitMethod" to arrayOf(java.lang.Long.TYPE, java.lang.Long.TYPE),
            "recordMethodCall" to arrayOf(java.lang.Long.TYPE),
            "recordCounter" to arrayOf(String::class.java, java.lang.Long.TYPE),
            "recordLogSpam" to arrayOf(String::class.java, String::class.java, Integer.TYPE),
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
                String::class.java,
                String::class.java,
            ),
            "exitAnnotatedContext" to arrayOf(Any::class.java),
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
        assertEquals(0L, JankHunterHooks.enterMethod(0L))
        JankHunterHooks.recordMethodCall(0L)
        JankHunterHooks.exitMethod(0L, 0L)
        assertEquals(0, calls.get())
    }
}
