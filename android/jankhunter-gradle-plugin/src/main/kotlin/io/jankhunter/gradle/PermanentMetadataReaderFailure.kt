package io.jankhunter.gradle

/** Digest or manifest mismatch; retrying open for the same reader id is pointless for the session. */
internal class PermanentMetadataReaderFailure(message: String, cause: Throwable? = null) : IllegalStateException(message, cause)
