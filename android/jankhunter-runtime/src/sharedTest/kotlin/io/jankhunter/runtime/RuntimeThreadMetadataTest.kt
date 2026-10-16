package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class RuntimeThreadMetadataTest {
    @Test
    fun inheritedPlatformAccessorsRemainAvailable() {
        val thread = object : Thread() {}
        assertEquals(thread.id, observedThreadId(thread))
        assertEquals(Thread.State.NEW, observedThreadState(thread))
    }

    @Test
    fun overriddenAccessorsAreNeverInvoked() {
        val thread = object : Thread() {
            override fun getId(): Long = error("host getter must not run")
            override fun getState(): State = error("host getter must not run")
        }
        assertEquals(0L, observedThreadId(thread))
        assertNull(observedThreadState(thread))
    }

    @Test
    fun oneUnsafeAccessorDoesNotDiscardTheOtherObservation() {
        val thread = object : Thread() { override fun getId(): Long = error("host getter") }
        assertEquals(0L, observedThreadId(thread))
        assertEquals(Thread.State.NEW, observedThreadState(thread))
    }
}
