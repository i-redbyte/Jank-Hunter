package io.jankhunter.runtime

import android.content.Context
import java.lang.reflect.Modifier
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterFacadeContractTest {
    @Test
    fun ioFacadeExposesOnlyFileAndContentOperations() {
        assertEquals(
            listOf("FILE_READ", "FILE_WRITE", "FILE_SYNC", "CONTENT_READ", "CONTENT_WRITE"),
            JankHunterIOOperation.entries.map(JankHunterIOOperation::name),
        )
    }

    @Test
    fun networkAdapterOwnsItsStaticJavaAbi() {
        assertStatic(
            JankHunterNetworkRuntime::class.java,
            "recordHttp",
            Void.TYPE,
            JankHunterHttpEvent::class.java,
        )
        assertStatic(
            JankHunterNetworkRuntime::class.java,
            "recordWebSocket",
            Void.TYPE,
            JankHunterWebSocketEvent::class.java,
        )
    }

    @Test
    fun generatedAutoInitHookOwnsItsStaticJavaAbi() {
        assertStatic(
            JankHunter::class.java,
            "autoInit",
            Void.TYPE,
            Context::class.java,
        )
    }

    private fun assertStatic(
        owner: Class<*>,
        name: String,
        returnType: Class<*>,
        vararg parameterTypes: Class<*>,
    ) {
        val method = owner.getDeclaredMethod(name, *parameterTypes)
        assertTrue("$name must remain a static Java facade", Modifier.isStatic(method.modifiers))
        assertEquals("$name return type", returnType, method.returnType)
    }
}
