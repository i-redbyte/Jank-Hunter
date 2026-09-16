package io.jankhunter.runtime

import java.lang.ref.WeakReference
import java.lang.reflect.Modifier
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeTokenAbiTest {
    @Test
    fun epochIdentityIsFinalAlongWithTheImmutableTokenMetadata() {
        for ((type, name) in listOf(
            JankHunterContextSnapshot::class.java to "collectionEpochId",
            JankHunterContextSnapshot::class.java to "httpToken",
            JankHunterDatabaseCallToken::class.java to "operationToken",
            JankHunterDatabaseTransactionToken::class.java to "collectionEpochId",
        )) {
            assertTrue("${type.simpleName}.$name must have constructor final-field publication semantics",
                Modifier.isFinal(type.getDeclaredField(name).modifiers))
        }
    }

    @Test
    fun contextSnapshotRetainsBothPreviouslyCompiledConstructorDescriptors() {
        val arguments = arrayOf(String::class.java, String::class.java, Boolean::class.javaPrimitiveType!!,
            Long::class.javaPrimitiveType!!, String::class.java, Long::class.javaPrimitiveType!!)
        JankHunterContextSnapshot::class.java.getDeclaredConstructor(*arguments)
        JankHunterContextSnapshot::class.java.getDeclaredConstructor(*arguments,
            Int::class.javaPrimitiveType!!, Class.forName("kotlin.jvm.internal.DefaultConstructorMarker"))
    }

    @Test
    fun manualDatabaseTokensRetainPreviouslyCompiledConstructorDescriptors() {
        JankHunterDatabaseCallToken::class.java.getDeclaredConstructor(
            Long::class.javaPrimitiveType, String::class.java, String::class.java, Long::class.javaPrimitiveType,
            Int::class.javaPrimitiveType, Int::class.javaPrimitiveType, Long::class.javaPrimitiveType)
        JankHunterDatabaseTransactionToken::class.java.getDeclaredConstructor(
            Long::class.javaPrimitiveType, Long::class.javaPrimitiveType, String::class.java,
            Long::class.javaPrimitiveType, Long::class.javaPrimitiveType, Long::class.javaPrimitiveType,
            WeakReference::class.java)
    }
}
