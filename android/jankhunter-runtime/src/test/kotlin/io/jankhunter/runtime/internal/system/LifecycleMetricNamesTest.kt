package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LifecycleMetricNamesTest {
    @Test
    fun screenNameIsSafeForMetricPath() {
        assertEquals("com_example_checkoutactivity", LifecycleMetricNames.screen("com.example.CheckoutActivity"))
    }

    @Test
    fun transitionIncludesBothNormalizedScreens() {
        val transition = LifecycleMetricNames.transition("Main Activity", "Checkout/Payment")

        assertEquals("main_activity.to.checkout_payment", transition)
    }

    @Test
    fun blankScreenFallsBackToUnknown() {
        assertEquals("unknown", LifecycleMetricNames.screen(" "))
    }

    @Test
    fun longScreenNamesKeepTheirCompleteIdentity() {
        val sharedPrefix = "screen." + "a".repeat(120)
        val first = LifecycleMetricNames.screen("$sharedPrefix.first")
        val second = LifecycleMetricNames.screen("$sharedPrefix.second")

        assertNotEquals(first, second)
        assertTrue(first.endsWith("_first"))
        assertTrue(second.endsWith("_second"))
    }
}
