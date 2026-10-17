package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ThreadIdentityRegistry
import java.lang.ref.WeakReference
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ThreadIdentityRegistrationTest {
    @Test fun rejectedOwnedRegistrationCanBeRetriedAfterCapacityIsFreed() {
        val references = mutableListOf<WeakReference<Thread>>()
        val identities = ThreadIdentityRegistry(limit = 1, weak = { WeakReference(it).also(references::add) })
        val thread = Thread.currentThread()
        val registration = ThreadIdentityRegistration(ownedByJankHunter = true)
        assertTrue(identities.register(1, thread, 1, "app.run"))

        assertFalse(registration.register(7, identities, 2, thread, 2, "Jank Hunter writer"))
        assertTrue(identities.dropped > 0)

        references.single().clear()
        identities.prune()
        assertTrue(registration.register(7, identities, 2, thread, 3, "Jank Hunter writer"))
        assertTrue(identities.observe(2, 4)?.ownedByJankHunter == true)
    }

    @Test fun rejectedExecutingRegistrationCanBeRetriedAfterCapacityIsFreed() {
        val references = mutableListOf<WeakReference<Thread>>()
        val identities = ThreadIdentityRegistry(limit = 1, weak = { WeakReference(it).also(references::add) })
        val thread = Thread.currentThread()
        val registration = ThreadIdentityRegistration(ownedByJankHunter = false)
        assertTrue(identities.register(1, thread, 1, "existing.run"))

        assertFalse(registration.register(7, identities, 2, thread, 2, "app.run"))
        assertFalse(registration.isRegistered(7))

        references.single().clear()
        identities.prune()
        assertTrue(registration.register(7, identities, 2, thread, 3, "app.run"))
        assertTrue(registration.isRegistered(7))
        assertFalse(identities.observe(2, 4)?.ownedByJankHunter ?: true)
    }

    @Test fun sameThreadRegistersAgainForNewCollectionEpoch() {
        val thread = Thread.currentThread()
        val registration = ThreadIdentityRegistration(ownedByJankHunter = true)
        val first = ThreadIdentityRegistry()
        val second = ThreadIdentityRegistry()

        assertTrue(registration.register(7, first, 1, thread, 1, "Jank Hunter graph"))
        assertTrue(registration.register(8, second, 1, thread, 2, "Jank Hunter graph"))
        assertTrue(second.observe(1, 3)?.ownedByJankHunter == true)
    }
}
