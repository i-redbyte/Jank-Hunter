package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.Opcodes

class RoomDaoInstrumentationPolicyTest {
    @Test
    fun selectsPublicDaoBoundaryAndPrivateStaticConnectionLambda() {
        val policy = RoomDaoInstrumentationPolicy(
            roomTracing = true,
            databaseTracing = true,
        )

        assertTrue(policy.isDaoBoundary(true, Opcodes.ACC_PUBLIC, "load"))
        assertFalse(policy.isDaoBoundary(true, Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "load"))
        assertFalse(policy.isDaoBoundary(true, Opcodes.ACC_PUBLIC, "getRequiredConverters"))

        val boundary = policy.sqlBoundary(
            true,
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC,
            "insertUsers\$lambda\$4",
            "(Landroidx/sqlite/SQLiteConnection;Ljava/lang/String;)V",
        )
        assertNotNull(boundary)
        assertEquals(1, boundary?.queryArgument)
        assertEquals(DatabaseOperationKind.INSERT, boundary?.operation)
    }

    @Test
    fun rejectsLambdaWithoutConnectionOrSqlArgument() {
        val policy = RoomDaoInstrumentationPolicy(true, true)

        assertEquals(
            null,
            policy.sqlBoundary(
                true,
                Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC,
                "deleteUsers\$lambda\$1",
                "(Ljava/lang/String;)V",
            ),
        )
    }
}
