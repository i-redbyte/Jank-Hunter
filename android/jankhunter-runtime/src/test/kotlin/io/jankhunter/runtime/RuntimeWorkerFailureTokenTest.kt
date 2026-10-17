package io.jankhunter.runtime

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeWorkerFailureTokenTest {
    @Test fun evidenceRequiresTheStillActiveWorkerToken() {
        val tokens = RuntimeAsyncTokenTable(RuntimeAsyncTokenIds())
        val token = tokens.begin(RuntimeAsyncTokenTable.WORKER, 7)
        assertTrue(tokens.isActive(token, RuntimeAsyncTokenTable.WORKER))
        assertFalse(tokens.isActive(token, RuntimeAsyncTokenTable.HTTP))
        tokens.claim(token, RuntimeAsyncTokenTable.WORKER)
        assertFalse(tokens.isActive(token, RuntimeAsyncTokenTable.WORKER))
        tokens.release(token)
        assertFalse(tokens.isActive(token, RuntimeAsyncTokenTable.WORKER))
    }
}
