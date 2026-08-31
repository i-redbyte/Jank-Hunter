package io.jankhunter.okhttp3

import org.junit.Assert.assertEquals
import org.junit.Test

class NetworkMetricNamesTest {
    @Test
    fun routeNormalizesVariablePathSegments() {
        val route = NetworkMetricNames.route(
            "GET",
            "/api/v1/users/123e4567-e89b-12d3-a456-426614174000/orders/42",
        )

        assertEquals("GET /api/v1/users/{id}/orders/{id}", route)
    }

    @Test
    fun routeUsesRootForEmptyPath() {
        assertEquals("POST /", NetworkMetricNames.route("POST", "/"))
    }

    @Test
    fun serviceAliasIsNormalizedOnceAndBounded() {
        assertEquals("mail_api_primary", NetworkMetricNames.serviceAlias(" Mail API / Primary "))
        assertEquals(null, NetworkMetricNames.serviceAlias("  "))
    }

    @Test
    fun routeStopsAtQueryOrFragmentWithoutParsingTheirSlashes() {
        assertEquals("GET /api/orders", NetworkMetricNames.route("GET", "/api/orders?next=/users/42"))
        assertEquals("GET /api/orders", NetworkMetricNames.route("GET", "/api/orders#next/users/42"))
    }

    @Test
    fun routeCollapsesUnsafeCharactersAndBoundsCardinality() {
        val route = NetworkMetricNames.route(
            "CUSTOM METHOD",
            "/api//orders---history/ABCDEF0123456789/${"item".repeat(20)}/ignored",
        )

        assertEquals("CUSTOMMETHOD /api/orders---history/{id}/${"item".repeat(20)}/ignored", route)
    }
}
