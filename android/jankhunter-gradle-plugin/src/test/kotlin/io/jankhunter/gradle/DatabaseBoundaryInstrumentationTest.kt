package io.jankhunter.gradle

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DatabaseBoundaryInstrumentationTest {
    @Test
    fun includesApplicationAndGeneratedDaoCallSites() {
        assertTrue(databaseBoundaryMatches("com.example.data.MessagesDao_Impl", emptySet()))
        assertTrue(databaseBoundaryMatches("third.party.feature.Storage", emptySet()))
    }

    @Test
    fun excludesDelegatedRoomAndSQLiteInternalsToPreventDoubleCounting() {
        assertFalse(databaseBoundaryMatches("androidx.room.RoomDatabase", emptySet()))
        assertFalse(databaseBoundaryMatches("androidx.room.util.DBUtil", emptySet()))
        assertFalse(databaseBoundaryMatches("androidx.sqlite.db.framework.FrameworkSQLiteDatabase", emptySet()))
    }

    @Test
    fun respectsUserExclusionsAndJankHunterRuntimeBoundary() {
        assertFalse(databaseBoundaryMatches("third.party.generated.Storage", setOf("third.party.generated")))
        assertFalse(databaseBoundaryMatches("io.jankhunter.runtime.JankHunter", emptySet()))
    }
}
