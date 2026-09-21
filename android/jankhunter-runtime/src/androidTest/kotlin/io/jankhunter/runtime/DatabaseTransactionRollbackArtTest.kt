package io.jankhunter.runtime

import android.database.sqlite.SQLiteDatabase
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.Jhlog
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class DatabaseTransactionRollbackArtTest {
    @Test
    fun trackedParentOutcomeMatchesActualSQLiteAfterNestedRollback() {
        val outcomes = linkedMapOf<Long, Long>()
        val tracker = DatabaseTransactionTracker(
            completionSink = DatabaseTransactionCompletionSink { id, _, _, _, _, outcome, _, _, _, _, _ -> outcomes[id] = outcome },
            nanoTime = System::nanoTime,
        )
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.targetContext.cacheDir, "sqlite-rollback-${System.nanoTime()}")
        val config = JankHunterConfig.builder().databaseTracingEnabled(true).autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val graph = RuntimeComponentGraph(
            nowMs = { SystemClock.elapsedRealtime() },
            nowUs = { SystemClock.elapsedRealtimeNanos() / 1_000L },
        )
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        val telemetry = graph.databaseTelemetry
        try {
            SQLiteDatabase.create(null).use { database ->
                database.execSQL("CREATE TABLE item (value INTEGER)")
                database.beginTransaction()
                val outer = tracker.begin(database, 1L, "outer", Jhlog.DATABASE_TRANSACTION_EXCLUSIVE)
                assertTrue(telemetry.beginTransaction(database, 1L, "outer", Jhlog.DATABASE_TRANSACTION_EXCLUSIVE.toInt()) > 0L)
                try {
                    database.execSQL("INSERT INTO item VALUES (1)")
                    database.beginTransaction()
                    tracker.begin(database, 2L, "inner", Jhlog.DATABASE_TRANSACTION_EXCLUSIVE)
                    assertTrue(telemetry.beginTransaction(database, 2L, "inner", Jhlog.DATABASE_TRANSACTION_EXCLUSIVE.toInt()) > 0L)
                    try {
                        database.execSQL("INSERT INTO item VALUES (2)")
                    } finally {
                        database.endTransaction()
                        tracker.finish(database, DatabaseFailureKind.OTHER, failed = false)
                        telemetry.endTransaction(database, null)
                    }
                    database.setTransactionSuccessful()
                    tracker.markSuccessful(database)
                    telemetry.markTransactionSuccessful(database)
                } finally {
                    database.endTransaction()
                    tracker.finish(database, DatabaseFailureKind.OTHER, failed = false)
                    telemetry.endTransaction(database, null)
                }
                val rows = database.rawQuery("SELECT COUNT(*) FROM item", null).use { cursor ->
                    cursor.moveToFirst()
                    cursor.getInt(0)
                }
                assertEquals("Platform SQLite did not roll back the outer insert", 0, rows)
                assertEquals("Telemetry contradicted SQLite result", Jhlog.DATABASE_TRANSACTION_ROLLBACK, outcomes[outer])
            }
            assertTrue(writer.close(1_000L))
            val log = directory.walkTopDown().single { it.isFile && it.extension == "jhlog" }
            log.copyTo(File(instrumentation.context.filesDir, "sqlite-nested-rollback.jhlog"), overwrite = true)
        } finally {
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }
}
