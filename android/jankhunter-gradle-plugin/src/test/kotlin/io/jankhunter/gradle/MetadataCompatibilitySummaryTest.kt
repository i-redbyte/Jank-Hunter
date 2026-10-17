package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MetadataCompatibilitySummaryTest {
    @Test fun incompleteCoverageAndBoundsAreExplicit() {
        val summary = MetadataCompatibilitySummary()
        repeat(1000) { n ->
            summary.accept("{\"class\":\"example.C$n\",\"metadataStatus\":\"UNSUPPORTED_VERSION\",\"metadataVersion\":\"99.$n\",\"metadataReader\":\"\"}")
        }
        val json = summary.toJson()
        assertTrue(json.contains("\"incomplete\":true"))
        assertTrue(json.contains("\"truncated\":true"))
        assertTrue(json.contains("OTHER"))
        assertTrue(json.toByteArray().size <= 512 * 1024)
    }

    @Test fun samplesAreBoundedAcrossVersionsOfTheSameStatusAndReader() {
        val summary = MetadataCompatibilitySummary()
        repeat(60) { n ->
            summary.accept("{\"class\":\"example.Sample$n\",\"metadataStatus\":\"UNSUPPORTED_VERSION\",\"metadataVersion\":\"99.$n\",\"metadataReader\":\"reader\"}")
        }
        org.junit.Assert.assertEquals(20, Regex("example.Sample").findAll(summary.toJson()).count())
    }

    @Test fun readableAndAbsentAreNotDegraded() {
        val summary = MetadataCompatibilitySummary()
        summary.accept("{\"class\":\"example.Java\",\"metadataStatus\":\"ABSENT\",\"metadataVersion\":\"\",\"metadataReader\":\"\"}")
        summary.accept("{\"class\":\"example.Kotlin\",\"metadataStatus\":\"READABLE\",\"metadataVersion\":\"2.3.0\",\"metadataReader\":\"kotlin-jvm-2.3.20\"}")
        assertFalse(summary.toJson().contains("\"incomplete\":true"))
    }
}
