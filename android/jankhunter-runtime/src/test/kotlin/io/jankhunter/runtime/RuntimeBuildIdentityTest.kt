package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.RuntimeBuildIdentity
import io.jankhunter.runtime.internal.io.RuntimeBuildIdentityResolver
import java.io.ByteArrayInputStream
import java.io.FileNotFoundException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertThrows
import org.junit.Test

class RuntimeBuildIdentityTest {
    private val namespace = "0123456789abcdef0123456789abcdef"
    private val digest = "ab".repeat(32)
    private val valid = "schema=1\nstate=mapped\nmapping-sha256=$digest\nsymbol-namespace=$namespace\n"

    @Test fun readsOnceAndClosesAsset() {
        var opens = 0
        var closed = false
        val resolver = RuntimeBuildIdentityResolver()
        val identity = resolver.resolve {
            opens++
            object : ByteArrayInputStream(valid.toByteArray()) {
                override fun close() { closed = true; super.close() }
            }
        }
        assertEquals(RuntimeBuildIdentity.Mapped(digest, namespace), identity)
        assertSame(identity, resolver.resolve { error("Must not reopen") })
        assertEquals(1, opens)
        assertEquals(true, closed)
    }

    @Test fun missingAssetRemainsExplicitAndCached() {
        val resolver = RuntimeBuildIdentityResolver()
        val identity = resolver.resolve { throw FileNotFoundException() }
        assertEquals(RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.MISSING), identity)
        assertSame(identity, resolver.resolve { error("Must not reopen missing asset") })
    }

    @Test fun assetProviderFailureDoesNotDisableRuntimeButFatalErrorsPropagate() {
        assertEquals(
            RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.UNREADABLE),
            RuntimeBuildIdentityResolver().resolve { throw IllegalStateException("assets unavailable") },
        )
        assertThrows(OutOfMemoryError::class.java) {
            RuntimeBuildIdentityResolver().resolve { throw OutOfMemoryError("fatal") }
        }
    }

    @Test fun rejectsDuplicateKeysTruncationInvalidDigestAndOversizedAsset() {
        for (text in listOf(valid + "state=mapped\n", valid.dropLast(1), valid.replace(digest, "short"), valid + "x".repeat(512))) {
            assertEquals(RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.MALFORMED), parse(text))
        }
    }

    @Test fun separatesUnminifiedFromUnknownAndUnsupportedSchema() {
        assertEquals(RuntimeBuildIdentity.Unminified(namespace), parse(valid.replace("state=mapped", "state=unminified").replace(digest, "")))
        assertEquals(RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.UNSUPPORTED_SCHEMA), parse(valid.replace("schema=1", "schema=2")))
        assertEquals(RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.MALFORMED), parse(valid.replace("state=mapped", "state=unminified")))
    }

    private fun parse(text: String) = RuntimeBuildIdentityResolver().resolve { ByteArrayInputStream(text.toByteArray()) }
}
