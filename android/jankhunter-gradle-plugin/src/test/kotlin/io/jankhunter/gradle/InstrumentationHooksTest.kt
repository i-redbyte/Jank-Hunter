package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationHooksTest {
    @Test
    fun matchesOkHttpHooksByExactSignature() {
        assertTrue(
            InstrumentationHooks.isOkHttpEventListenerFactory(
                "okhttp3/OkHttpClient\$Builder",
                "eventListenerFactory",
                "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
            ),
        )
        assertTrue(
            InstrumentationHooks.isOkHttpNewWebSocket(
                "okhttp3/OkHttpClient",
                "newWebSocket",
                "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
            ),
        )
        assertFalse(
            InstrumentationHooks.isOkHttpEventListenerFactory(
                "okhttp3/OkHttpClient\$Builder",
                "build",
                "()Lokhttp3/OkHttpClient;",
            ),
        )
    }

    @Test
    fun classifiesHandlerSafeSubset() {
        assertEquals(
            HandlerRunnableKind.SINGLE_RUNNABLE,
            InstrumentationHooks.handlerRunnableKind(
                "android/os/Handler",
                "post",
                "(Ljava/lang/Runnable;)Z",
            ),
        )
        assertEquals(
            HandlerRunnableKind.RUNNABLE_LONG,
            InstrumentationHooks.handlerRunnableKind(
                "android/os/Handler",
                "postDelayed",
                "(Ljava/lang/Runnable;J)Z",
            ),
        )
        assertEquals(
            HandlerRunnableKind.RUNNABLE_OBJECT_LONG,
            InstrumentationHooks.handlerRunnableKind(
                "android/os/Handler",
                "postAtTime",
                "(Ljava/lang/Runnable;Ljava/lang/Object;J)Z",
            ),
        )
        assertTrue(
            InstrumentationHooks.isHandlerMessageSend(
                "android/os/Handler",
                "sendMessageDelayed",
                "(Landroid/os/Message;J)Z",
            ),
        )
        assertFalse(
            InstrumentationHooks.isHandlerMessageSend(
                "android/os/Handler",
                "obtainMessage",
                "()Landroid/os/Message;",
            ),
        )
    }

    @Test
    fun classifiesExecutorSafeSubset() {
        assertEquals(
            ExecutorRunnableKind.SINGLE_RUNNABLE,
            InstrumentationHooks.executorRunnableKind(
                "java/util/concurrent/Executor",
                "execute",
                "(Ljava/lang/Runnable;)V",
            ),
        )
        assertEquals(
            ExecutorRunnableKind.RUNNABLE_OBJECT,
            InstrumentationHooks.executorRunnableKind(
                "java/util/concurrent/ExecutorService",
                "submit",
                "(Ljava/lang/Runnable;Ljava/lang/Object;)Ljava/util/concurrent/Future;",
            ),
        )
        assertEquals(
            ExecutorRunnableKind.RUNNABLE_LONG_LONG_OBJECT,
            InstrumentationHooks.executorRunnableKind(
                "java/util/concurrent/ScheduledExecutorService",
                "scheduleWithFixedDelay",
                "(Ljava/lang/Runnable;JJLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;",
            ),
        )
        assertEquals(
            ExecutorCallableKind.SINGLE_CALLABLE,
            InstrumentationHooks.executorCallableKind(
                "java/util/concurrent/ExecutorService",
                "submit",
                "(Ljava/util/concurrent/Callable;)Ljava/util/concurrent/Future;",
            ),
        )
        assertEquals(
            ExecutorCallableKind.CALLABLE_LONG_OBJECT,
            InstrumentationHooks.executorCallableKind(
                "java/util/concurrent/ScheduledExecutorService",
                "schedule",
                "(Ljava/util/concurrent/Callable;JLjava/util/concurrent/TimeUnit;)Ljava/util/concurrent/ScheduledFuture;",
            ),
        )
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
}
