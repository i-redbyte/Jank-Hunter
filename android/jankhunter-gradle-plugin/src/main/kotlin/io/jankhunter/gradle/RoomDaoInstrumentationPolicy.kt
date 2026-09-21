package io.jankhunter.gradle

import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

internal data class RoomSqlBoundary(
    val queryArgument: Int,
    val operation: DatabaseOperationKind,
)

internal class RoomDaoInstrumentationPolicy(
    private val roomTracing: Boolean,
    private val databaseTracing: Boolean,
) {
    fun isDaoBoundary(roomDatabaseFieldPresent: Boolean, access: Int, name: String): Boolean {
        val excludedFlags = Opcodes.ACC_STATIC or Opcodes.ACC_SYNTHETIC or Opcodes.ACC_BRIDGE
        return roomTracing &&
            roomDatabaseFieldPresent &&
            access and Opcodes.ACC_PUBLIC != 0 &&
            access and excludedFlags == 0 &&
            name != "<init>" &&
            name != "getRequiredConverters"
    }

    fun sqlBoundary(
        roomDatabaseFieldPresent: Boolean,
        access: Int,
        name: String,
        descriptor: String,
    ): RoomSqlBoundary? {
        if (!databaseTracing || !roomDatabaseFieldPresent || "\$lambda\$" !in name) return null
        val requiredAccess = Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC
        if (access and requiredAccess != requiredAccess) return null
        val arguments = Type.getArgumentTypes(descriptor)
        if (arguments.none { it.sort == Type.OBJECT && it.internalName == ANDROIDX_SQLITE_CONNECTION }) return null
        val queryArgument = arguments.indexOfFirst {
            it.sort == Type.OBJECT && it.internalName == JAVA_LANG_STRING
        }
        if (queryArgument < 0) return null
        return RoomSqlBoundary(queryArgument, operation(name))
    }

    private fun operation(methodName: String): DatabaseOperationKind {
        val source = methodName.substringBefore("\$lambda\$").lowercase()
        return when {
            source.startsWith("insert") || source.startsWith("upsert") || source.startsWith("add") ->
                DatabaseOperationKind.INSERT
            source.startsWith("delete") || source.startsWith("remove") || source.startsWith("clear") ->
                DatabaseOperationKind.DELETE
            source.startsWith("update") || source.startsWith("set") || source.startsWith("mark") ->
                DatabaseOperationKind.UPDATE
            else -> DatabaseOperationKind.QUERY
        }
    }

    private companion object {
        const val ANDROIDX_SQLITE_CONNECTION = "androidx/sqlite/SQLiteConnection"
        const val JAVA_LANG_STRING = "java/lang/String"
    }
}
