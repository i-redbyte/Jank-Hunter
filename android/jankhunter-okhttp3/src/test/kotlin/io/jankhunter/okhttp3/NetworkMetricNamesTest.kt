package io.jankhunter.okhttp3

import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.InterruptedIOException

class NetworkMetricNamesTest {
    @Test
    fun routeNormalizesVariablePathSegments() {
        val route = NetworkMetricNames.route(
            "GET",
            "/api/v1/users/123e4567-e89b-12d3-a456-426614174000/orders/42",
        )

        assertEquals("get_api_v1_users_id_orders_id", route)
    }

    @Test
    fun routeUsesRootForEmptyPath() {
        assertEquals("post_root", NetworkMetricNames.route("POST", "/"))
    }

    @Test
    fun ownerMetricKeyIsStableAndSafe() {
        assertEquals("checkout_screen", NetworkMetricNames.owner("Checkout Screen!"))
    }

    @Test
    fun throwableMetricKeyUsesExceptionClassName() {
        assertEquals("interruptedioexception", NetworkMetricNames.throwable(InterruptedIOException()))
    }
}
