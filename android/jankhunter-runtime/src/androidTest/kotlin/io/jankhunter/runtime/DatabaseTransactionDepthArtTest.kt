package io.jankhunter.runtime

import android.database.sqlite.SQLiteDatabase
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.Jhlog
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DatabaseTransactionDepthArtTest {
    @Test
    fun deepTransactionHooksMatchSQLiteAndProduceCompleteWireLineage() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.filesDir, "database-depth-wire")
        JankHunter.shutdown()
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .databaseTracingEnabled(true).runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        try {
            instrumentation.runOnMainSync {
                JankHunter.init(instrumentation.targetContext, config)
                for (rollback in booleanArrayOf(false, true)) runDatabase(rollback)
            }
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals(1, files.size)
        files.single().copyTo(File(instrumentation.context.filesDir, "database-depth-5.1.0.jhlog"), overwrite = true)
    }

    private fun runDatabase(rollback: Boolean) = SQLiteDatabase.create(null).use { database ->
        database.execSQL("CREATE TABLE item (value INTEGER)")
        var active = 0
        try {
            repeat(DEPTH) { level ->
                database.beginTransaction()
                active++
                val source = 7_000L + (if (rollback) 100L else 0L) + level
                JankHunterHooks.beginDatabaseTransaction(database, source, "depth.$source", Jhlog.DATABASE_TRANSACTION_EXCLUSIVE.toInt())
                val token = JankHunterHooks.enterDatabase()
                assertTrue(token > 0L)
                database.execSQL("INSERT INTO item VALUES (?)", arrayOf(level))
                JankHunterHooks.exitDatabase(
                    token, source, "depth.$source", "INSERT INTO item VALUES (?)", 123L,
                    Jhlog.DATABASE_FRAMEWORK_SQLITE.toInt(), Jhlog.DATABASE_OPERATION_INSERT.toInt(),
                    Jhlog.DATABASE_BOUNDARY_EXECUTE.toInt(), false, 0, 0, 0L, true, null,
                )
                // Keep the wire fixture below bounded writer admission; the transaction stays active.
                if (level and 15 == 15) JankHunter.flush()
            }
            repeat(DEPTH) { ended ->
                if (!rollback || ended > 0) {
                    database.setTransactionSuccessful()
                    JankHunterHooks.markDatabaseTransactionSuccessful(database)
                }
                database.endTransaction()
                active--
                JankHunterHooks.endDatabaseTransaction(database, null)
                if (ended and 15 == 15) JankHunter.flush()
            }
            JankHunter.flush()
            val count = database.rawQuery("SELECT COUNT(*) FROM item", null).use { cursor ->
                assertTrue(cursor.moveToFirst())
                cursor.getInt(0)
            }
            assertEquals(if (rollback) 0 else DEPTH, count)
        } finally {
            repeat(active) {
                database.endTransaction()
                JankHunterHooks.endDatabaseTransaction(database, null)
            }
        }
    }

    private companion object {
        const val DEPTH = 65
    }
}
