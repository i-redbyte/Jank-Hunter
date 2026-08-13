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
    fun routeStopsAtQueryOrFragmentWithoutParsingTheirSlashes() {
        assertEquals("get_api_orders", NetworkMetricNames.route("GET", "/api/orders?next=/users/42"))
        assertEquals("get_api_orders", NetworkMetricNames.route("GET", "/api/orders#next/users/42"))
    }

    @Test
    fun routeCollapsesUnsafeCharactersAndBoundsCardinality() {
        val route = NetworkMetricNames.route(
            "CUSTOM METHOD",
            "/api//orders---history/ABCDEF0123456789/${"item".repeat(20)}/ignored",
        )

        assertEquals("custom_method_api_orders_history_id_${"item".repeat(15)}", route)
        assertEquals(96, route.length)
    }

    @Test
    fun throwableMetricKeyUsesExceptionClassName() {
        assertEquals("interruptedioexception", NetworkMetricNames.throwable(InterruptedIOException()))
    }
}
