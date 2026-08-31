package io.jankhunter.runtime

import android.os.DeadObjectException
import android.os.RemoteException
import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.TimeoutException
import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeBinderTelemetryTest {
    @Test
    fun failureTaxonomyUsesThrowableTypesAndNeverMessages() {
        assertEquals(Jhlog.BINDER_FAILURE_DEAD_OBJECT, binderFailureKind(DeadObjectException()))
        assertEquals(Jhlog.BINDER_FAILURE_REMOTE, binderFailureKind(RemoteException("private")))
        assertEquals(Jhlog.BINDER_FAILURE_SECURITY, binderFailureKind(SecurityException("private")))
        assertEquals(Jhlog.BINDER_FAILURE_TIMEOUT, binderFailureKind(TimeoutException("private")))
        assertEquals(Jhlog.BINDER_FAILURE_OTHER, binderFailureKind(IllegalStateException("private")))
    }

    @Test
    fun onewayFlagUsesOnlyPublicBinderFlagBit() {
        assertEquals(Jhlog.BINDER_FLAG_ONEWAY, binderEventFlags(1))
        assertEquals(0L, binderEventFlags(0))
        assertEquals(Jhlog.BINDER_FLAG_ONEWAY, binderEventFlags(3))
    }
}
