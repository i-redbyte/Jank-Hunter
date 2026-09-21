package io.jankhunter.sample.graph

import io.jankhunter.runtime.JankHunterDatabaseTracing

import android.database.sqlite.SQLiteConstraintException
import android.os.SystemClock
import androidx.room.ColumnInfo
import androidx.room.Dao
import androidx.room.Database
import androidx.room.Entity
import androidx.room.PrimaryKey
import androidx.room.Query
import androidx.room.Room
import androidx.room.RoomDatabase
import io.jankhunter.sample.SampleApplication
import io.jankhunter.runtime.JankHunterDatabaseOperation
import io.jankhunter.runtime.JankHunterDatabasePhase
import io.jankhunter.runtime.JankHunterDatabaseResultKind

@Entity(tableName = "sample_database_record")
internal data class SampleDatabaseRecord(
    @PrimaryKey val id: Long,
    @ColumnInfo(name = "group_key", index = true) val groupKey: Long,
    val label: String,
)

@Dao
internal interface SampleDatabaseDao {
    @Query("SELECT * FROM sample_database_record WHERE id = :id")
    fun load(id: Long): SampleDatabaseRecord?

    @Query("SELECT length(randomblob(:bytes))")
    fun allocateRandomBlob(bytes: Int): Int
}

@Database(
    entities = [SampleDatabaseRecord::class],
    version = 1,
    exportSchema = false,
)
internal abstract class SampleRoomDatabase : RoomDatabase() {
    abstract fun records(): SampleDatabaseDao

    companion object {
        const val NAME = "jankhunter-database-scenario.db"

        fun create(application: SampleApplication): SampleRoomDatabase {
            return Room.databaseBuilder(application, SampleRoomDatabase::class.java, NAME)
                .allowMainThreadQueries()
                .build()
        }
    }
}

internal class RoomDatabaseScenarioStore(
    private val database: SampleRoomDatabase,
) : DatabaseScenarioStore {
    private val dao = database.records()

    override fun resetAndSeed() {
        val sqlite = database.openHelper.writableDatabase
        sqlite.execSQL(DELETE_ALL_SQL)
        sqlite.execSQL(TRANSACTION_SEED_SQL)
    }

    override fun runSlowMainQuery() {
        check(dao.allocateRandomBlob(MAIN_RANDOM_BLOB_BYTES) == MAIN_RANDOM_BLOB_BYTES)
    }

    override fun loadRecord(id: Long): Boolean {
        return dao.load(id) != null
    }

    override fun executePreparedUpdates(): Int {
        val statement = database.openHelper.writableDatabase.compileStatement(PREPARED_UPDATE_SQL)
        var affected = 0
        try {
            for (id in 1L..PREPARED_UPDATE_COUNT) {
                statement.bindString(1, "updated")
                statement.bindLong(2, id)
                affected += statement.executeUpdateDelete()
                statement.clearBindings()
            }
        } finally {
            statement.close()
        }
        return affected
    }

    override fun replaceInTransaction(): Boolean {
        val sqlite = database.openHelper.writableDatabase
        sqlite.beginTransactionNonExclusive()
        return try {
            sqlite.execSQL(DELETE_ALL_SQL)
            sqlite.execSQL(TRANSACTION_SEED_SQL)
            sqlite.setTransactionSuccessful()
            true
        } finally {
            sqlite.endTransaction()
        }
    }

    override fun observeExpectedConstraintFailure(): Boolean {
        val statement = database.openHelper.writableDatabase.compileStatement(CONSTRAINT_FAILURE_SQL)
        return try {
            statement.bindLong(1, 1L)
            statement.bindLong(2, 1L)
            statement.bindString(3, "duplicate")
            statement.executeInsert()
            false
        } catch (_: SQLiteConstraintException) {
            true
        } finally {
            statement.close()
        }
    }

    override fun runSlowBackgroundQuery() {
        check(dao.allocateRandomBlob(BACKGROUND_RANDOM_BLOB_BYTES) == BACKGROUND_RANDOM_BLOB_BYTES)
    }

    override fun runCustomAdapterProbe() {
        val token = JankHunterDatabaseTracing.beginCall(
            sourceAlias = "sample.custom_database_adapter",
            sqlTemplate = "SELECT payload FROM custom_cache WHERE group_key = ? ORDER BY sequence",
            operation = JankHunterDatabaseOperation.QUERY,
        )
        if (token == null) {
            SystemClock.sleep(CUSTOM_EXECUTE_MS + CUSTOM_MATERIALIZE_MS)
            return
        }
        val execute = JankHunterDatabaseTracing.beginPhase(token)
        SystemClock.sleep(CUSTOM_EXECUTE_MS)
        JankHunterDatabaseTracing.endPhase(token, JankHunterDatabasePhase.EXECUTE, execute)
        val materialize = JankHunterDatabaseTracing.beginPhase(token)
        SystemClock.sleep(CUSTOM_MATERIALIZE_MS)
        JankHunterDatabaseTracing.endPhase(token, JankHunterDatabasePhase.MATERIALIZE, materialize)
        JankHunterDatabaseTracing.endCall(
            token = token,
            resultKind = JankHunterDatabaseResultKind.ROWS,
            resultCount = 1L,
        )
    }

    override fun close() {
        database.close()
    }

    private companion object {
        const val PREPARED_UPDATE_COUNT = 3L
        const val MAIN_RANDOM_BLOB_BYTES = 8 * 1024 * 1024
        const val BACKGROUND_RANDOM_BLOB_BYTES = 32 * 1024 * 1024
        const val CUSTOM_EXECUTE_MS = 120L
        const val CUSTOM_MATERIALIZE_MS = 30L
        const val DELETE_ALL_SQL = "DELETE FROM sample_database_record"
        const val PREPARED_UPDATE_SQL =
            "UPDATE sample_database_record SET label = ? WHERE id = ?"
        const val CONSTRAINT_FAILURE_SQL =
            "INSERT OR ABORT INTO sample_database_record(id, group_key, label) VALUES (?, ?, ?)"
        const val TRANSACTION_SEED_SQL =
            "WITH RECURSIVE seed(id) AS (" +
                "SELECT 1 UNION ALL SELECT id + 1 FROM seed WHERE id < 64" +
                ") INSERT INTO sample_database_record(id, group_key, label) " +
                "SELECT id, id % 8, 'transaction-' || id FROM seed"
    }
}
