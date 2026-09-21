package io.jankhunter.gradle

import io.jankhunter.sql.SqlNormalizer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.InsnNode
import org.objectweb.asm.tree.LdcInsnNode
import org.objectweb.asm.tree.MethodInsnNode
import org.objectweb.asm.tree.MethodNode

class DatabaseSqlOriginAnalyzerTest {
    @Test
    fun preservesInvocationOrderAndNormalizesOnlyDatabaseLiteral() {
        val query = " SELECT  *  FROM messages WHERE id = 42 "
        val method = MethodNode(Opcodes.ASM9, Opcodes.ACC_STATIC, "load", "()V", null, null).apply {
            instructions.add(MethodInsnNode(Opcodes.INVOKESTATIC, "java/lang/System", "nanoTime", "()J", false))
            instructions.add(InsnNode(Opcodes.POP2))
            instructions.add(InsnNode(Opcodes.ACONST_NULL))
            instructions.add(LdcInsnNode(query))
            instructions.add(InsnNode(Opcodes.ACONST_NULL))
            instructions.add(
                MethodInsnNode(
                    Opcodes.INVOKEVIRTUAL,
                    "android/database/sqlite/SQLiteDatabase",
                    "rawQuery",
                    "(Ljava/lang/String;[Ljava/lang/String;)Landroid/database/Cursor;",
                    false,
                ),
            )
            instructions.add(InsnNode(Opcodes.POP))
            instructions.add(InsnNode(Opcodes.RETURN))
            maxStack = 3
            maxLocals = 0
        }

        val origins = analyzeDatabaseInvocationOrigins("example/Queries", method)

        assertEquals(2, origins.size)
        assertEquals("nanoTime", origins[0].name)
        assertNull(origins[0].normalizedLiteral)
        assertEquals("rawQuery", origins[1].name)
        assertEquals(SqlNormalizer.normalize(query), origins[1].normalizedLiteral)
    }

    @Test
    fun keepsNonDatabaseInvocationWithoutRequestingSqlSourceFrames() {
        val method = MethodNode(Opcodes.ASM9, Opcodes.ACC_STATIC, "clock", "()V", null, null).apply {
            instructions.add(MethodInsnNode(Opcodes.INVOKESTATIC, "java/lang/System", "nanoTime", "()J", false))
        }

        val origin = analyzeDatabaseInvocationOrigins("example/Clock", method).single()

        assertEquals("nanoTime", origin.name)
        assertNull(origin.normalizedLiteral)
    }
}
