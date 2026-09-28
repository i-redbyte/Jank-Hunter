package io.jankhunter.runtime.internal.io

import java.io.File
import java.time.Instant
import java.util.TimeZone
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class SessionArtifactPathTest {
    @Test
    fun sessionDirectoryNameContainsUtcStartIndexAndFullRunId() {
        val original = TimeZone.getDefault()
        try {
            TimeZone.setDefault(TimeZone.getTimeZone("Pacific/Kiritimati"))

            val name = SessionArtifactPath.sessionDirectoryName(
                startedAtUnixMs = Instant.parse("2027-01-02T03:04:05.006Z").toEpochMilli(),
                dailySessionIndex = 17L,
                runId = RUN_ID,
            )

            assertEquals(
                "2027-01-02T03-04-05.006Z_17_000102030405060708090a0b0c0d0e0f",
                name,
            )
        } finally {
            TimeZone.setDefault(original)
        }
    }

    @Test
    fun processDirectoryNameIsFullCanonicalInstanceId() {
        assertEquals(
            "0f0e0d0c0b0a09080706050403020100",
            SessionArtifactPath.processDirectoryName(RUN_ID.reversedArray()),
        )
    }

    @Test
    fun canonicalSessionDirectoryRoundTripsWithoutFilesystemMetadata() {
        val startedAt = Instant.parse("2027-01-02T03:04:05.006Z").toEpochMilli()
        val name = SessionArtifactPath.sessionDirectoryName(startedAt, 17L, RUN_ID)

        val parsed = requireNotNull(SessionArtifactPath.parseSessionDirectoryName(name))

        assertEquals(startedAt, parsed.startedAtUnixMs)
        assertEquals(17L, parsed.dailySessionIndex)
        assertEquals("000102030405060708090a0b0c0d0e0f", parsed.runId)
        assertEquals(null, SessionArtifactPath.parseSessionDirectoryName("sessions/$name"))
        assertEquals(null, SessionArtifactPath.parseSessionDirectoryName("$name.jhlog.zip"))
    }

    @Test
    fun scopePlacesProcessDirectlyBelowSession() {
        val root = File("root")
        val scope = SessionArtifactPath.scope(root, 0L, 3L, RUN_ID, RUN_ID.reversedArray())

        assertEquals(File(root, "1970-01-01T00-00-00.000Z_3_000102030405060708090a0b0c0d0e0f"), scope.sessionDirectory)
        assertEquals(
            File(scope.sessionDirectory, "0f0e0d0c0b0a09080706050403020100"),
            scope.processDirectory,
        )
    }

    @Test
    fun invalidIdentityIsRejectedBeforeFilesystemAccess() {
        assertThrows(IllegalArgumentException::class.java) {
            SessionArtifactPath.sessionDirectoryName(-1L, 0L, RUN_ID)
        }
        assertThrows(IllegalArgumentException::class.java) {
            SessionArtifactPath.sessionDirectoryName(0L, -1L, RUN_ID)
        }
        assertThrows(IllegalArgumentException::class.java) {
            SessionArtifactPath.sessionDirectoryName(0L, 0L, ByteArray(15))
        }
        assertThrows(IllegalArgumentException::class.java) {
            SessionArtifactPath.processDirectoryName(ByteArray(16))
        }
        assertThrows(IllegalArgumentException::class.java) {
            SessionArtifactPath.sessionDirectoryName(Long.MAX_VALUE, 0L, RUN_ID)
        }
    }

    private companion object {
        val RUN_ID = ByteArray(16) { index -> index.toByte() }
    }
}
