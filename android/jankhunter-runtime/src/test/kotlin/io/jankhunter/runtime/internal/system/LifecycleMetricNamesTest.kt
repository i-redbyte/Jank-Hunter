package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
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
}
