package io.jankhunter.gradle

internal data class VersionedBridgeSignature(
    val id: String,
    val intent: HookIntent,
    val owners: Set<String>,
    val names: Set<String>,
    val roles: Map<ArgumentRole, Int> = emptyMap(),
    private val descriptorMatcher: (MethodCall) -> Boolean,
) {
    fun matches(call: MethodCall): Boolean {
        return call.matchesOwner(owners) &&
            call.name in names &&
            descriptorMatcher(call)
    }

    companion object {
        fun exact(signature: IntentSignature): VersionedBridgeSignature {
            return exact(signature.spec, signature.intent)
        }

        fun exact(spec: SignatureSpec, intent: HookIntent): VersionedBridgeSignature {
            return VersionedBridgeSignature(
                id = spec.id,
                intent = intent,
                owners = spec.owners,
                names = spec.names,
                roles = spec.roles,
                descriptorMatcher = { call -> call.descriptor in spec.descriptors },
            )
        }

        fun covariantObjectReturn(signature: IntentSignature): VersionedBridgeSignature {
            val spec = signature.spec
            val parameterDescriptors = spec.descriptors.mapTo(linkedSetOf()) { descriptor ->
                descriptor.substring(0, descriptor.lastIndexOf(')') + 1)
            }
            return VersionedBridgeSignature(
                id = spec.id,
                intent = signature.intent,
                owners = spec.owners,
                names = spec.names,
                roles = spec.roles,
                descriptorMatcher = { call ->
                    val argumentsEnd = call.descriptor.lastIndexOf(')')
                    argumentsEnd >= 0 &&
                        call.descriptor.substring(0, argumentsEnd + 1) in parameterDescriptors &&
                        call.descriptor.substring(argumentsEnd + 1).let { result ->
                            result.startsWith('L') && result.endsWith(';')
                        }
                },
            )
        }
    }
}

internal data class VersionedBridgeMatch(
    val bridgeId: String,
    val signature: VersionedBridgeSignature,
) {
    val intent: HookIntent
        get() = signature.intent

    fun toDecision(): HookDecision.Matched {
        return HookDecision.Matched(
            intent = signature.intent,
            signatureId = signature.id,
            bridgeId = bridgeId,
        )
    }
}

internal interface VersionedInstrumentationBridge {
    val id: String
    val family: String
    val signatures: List<VersionedBridgeSignature>

    fun relevant(call: MethodCall): Boolean {
        return signatures.any { signature ->
            call.matchesOwner(signature.owners) && call.name in signature.names
        }
    }

    fun match(call: MethodCall): VersionedBridgeMatch? {
        val signature = signatures.firstOrNull { it.matches(call) } ?: return null
        return VersionedBridgeMatch(id, signature)
    }
}

internal fun interface InstrumentationBridgeProvider {
    fun bridges(): List<VersionedInstrumentationBridge>
}

internal object DefaultInstrumentationBridgeProvider : InstrumentationBridgeProvider {
    override fun bridges(): List<VersionedInstrumentationBridge> = listOf(
        OkHttp3Bridge,
        AndroidHandlerBridge,
        JdkExecutorBridge,
        KotlinxCoroutinesBridge,
        AndroidViewInteractionOperationBridge,
        AndroidLogSpamBridge,
        TimberLogSpamBridge,
        CriticalIOBridge,
        DatabaseBridgeV1,
    )
}

internal class InstrumentationBridgeRegistry(
    providers: List<InstrumentationBridgeProvider>,
) {
    private val bridges: List<VersionedInstrumentationBridge> = providers.flatMap { it.bridges() }
    private val byFamily: Map<String, List<VersionedInstrumentationBridge>> = bridges.groupBy { it.family }

    fun family(family: String): List<VersionedInstrumentationBridge> = byFamily[family].orEmpty()

    fun all(): List<VersionedInstrumentationBridge> = bridges
}

internal object VersionedBridgeCatalog {
    private val registry = InstrumentationBridgeRegistry(listOf(DefaultInstrumentationBridgeProvider))
    val okHttp: List<VersionedInstrumentationBridge> = registry.family("okhttp")
    val handlers: List<VersionedInstrumentationBridge> = registry.family("handler")
    val executors: List<VersionedInstrumentationBridge> = registry.family("executor")
    val coroutines: List<VersionedInstrumentationBridge> = registry.family("coroutines")
    val interactionOperations: List<VersionedInstrumentationBridge> = registry.family("interaction-operation")
    val logSpam: List<VersionedInstrumentationBridge> = registry.family("logspam")
    val io: List<VersionedInstrumentationBridge> = registry.family("io")
    val database: List<VersionedInstrumentationBridge> = registry.family("database")

    fun matchOkHttp(call: MethodCall, intents: Set<String>): VersionedBridgeMatch? {
        return okHttp.firstNotNullOfOrNull { bridge ->
            bridge.match(call)?.takeIf { it.intent.id in intents }
        }
    }

    fun all(): List<VersionedInstrumentationBridge> = registry.all()
}

private object DatabaseBridgeV1 : VersionedInstrumentationBridge {
    override val id: String = "android.database.bridge.v1"
    override val family: String = "database"
    override val signatures: List<VersionedBridgeSignature> = listOf(
        exact(
            "android.sqlite_database.raw_query",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("rawQuery"),
            setOf(
                "(Ljava/lang/String;[Ljava/lang/String;)Landroid/database/Cursor;",
                "(Ljava/lang/String;[Ljava/lang/String;Landroid/os/CancellationSignal;)Landroid/database/Cursor;",
            ),
        ),
        exact(
            "android.sqlite_database.raw_query_factory",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("rawQueryWithFactory"),
            setOf(
                "(Landroid/database/sqlite/SQLiteDatabase\$CursorFactory;Ljava/lang/String;[Ljava/lang/String;Ljava/lang/String;)Landroid/database/Cursor;",
                "(Landroid/database/sqlite/SQLiteDatabase\$CursorFactory;Ljava/lang/String;[Ljava/lang/String;Ljava/lang/String;Landroid/os/CancellationSignal;)Landroid/database/Cursor;",
            ),
        ),
        exact(
            "android.sqlite_database.exec",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("execSQL"),
            setOf("(Ljava/lang/String;)V", "(Ljava/lang/String;[Ljava/lang/Object;)V"),
        ),
        exact(
            "android.sqlite_database.compile",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("compileStatement"),
            setOf("(Ljava/lang/String;)Landroid/database/sqlite/SQLiteStatement;"),
        ),
        exact(
            "android.sqlite_database.insert",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("insert", "insertOrThrow", "replace", "replaceOrThrow"),
            setOf("(Ljava/lang/String;Ljava/lang/String;Landroid/content/ContentValues;)J"),
        ),
        exact(
            "android.sqlite_database.insert_conflict",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("insertWithOnConflict"),
            setOf("(Ljava/lang/String;Ljava/lang/String;Landroid/content/ContentValues;I)J"),
        ),
        exact(
            "android.sqlite_database.update",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("update"),
            setOf("(Ljava/lang/String;Landroid/content/ContentValues;Ljava/lang/String;[Ljava/lang/String;)I"),
        ),
        exact(
            "android.sqlite_database.delete",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("delete"),
            setOf("(Ljava/lang/String;Ljava/lang/String;[Ljava/lang/String;)I"),
        ),
        exact(
            "android.sqlite_database.update_conflict",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("updateWithOnConflict"),
            setOf("(Ljava/lang/String;Landroid/content/ContentValues;Ljava/lang/String;[Ljava/lang/String;I)I"),
        ),
        exact(
            "android.sqlite_statement.execute",
            setOf("android/database/sqlite/SQLiteStatement"),
            setOf("execute"),
            setOf("()V"),
        ),
        exact(
            "android.sqlite_statement.execute_insert",
            setOf("android/database/sqlite/SQLiteStatement"),
            setOf("executeInsert"),
            setOf("()J"),
        ),
        exact(
            "android.sqlite_statement.execute_update_delete",
            setOf("android/database/sqlite/SQLiteStatement"),
            setOf("executeUpdateDelete"),
            setOf("()I"),
        ),
        exact(
            "android.sqlite_statement.query_long",
            setOf("android/database/sqlite/SQLiteStatement"),
            setOf("simpleQueryForLong"),
            setOf("()J"),
        ),
        exact(
            "android.sqlite_statement.query_string",
            setOf("android/database/sqlite/SQLiteStatement"),
            setOf("simpleQueryForString"),
            setOf("()Ljava/lang/String;"),
        ),
        exact(
            "androidx.support_sqlite_database.query",
            setOf("androidx/sqlite/db/SupportSQLiteDatabase", "androidx/room/RoomDatabase"),
            setOf("query"),
            setOf(
                "(Ljava/lang/String;)Landroid/database/Cursor;",
                "(Ljava/lang/String;[Ljava/lang/Object;)Landroid/database/Cursor;",
                "(Landroidx/sqlite/db/SupportSQLiteQuery;)Landroid/database/Cursor;",
                "(Landroidx/sqlite/db/SupportSQLiteQuery;Landroid/os/CancellationSignal;)Landroid/database/Cursor;",
            ),
        ),
        exact(
            "androidx.support_sqlite_database.exec",
            setOf("androidx/sqlite/db/SupportSQLiteDatabase", "androidx/room/RoomDatabase"),
            setOf("execSQL"),
            setOf("(Ljava/lang/String;)V", "(Ljava/lang/String;[Ljava/lang/Object;)V"),
        ),
        exact(
            "androidx.support_sqlite_database.compile",
            setOf("androidx/sqlite/db/SupportSQLiteDatabase", "androidx/room/RoomDatabase"),
            setOf("compileStatement"),
            setOf("(Ljava/lang/String;)Landroidx/sqlite/db/SupportSQLiteStatement;"),
        ),
        exact(
            "androidx.support_sqlite_statement.execute",
            setOf("androidx/sqlite/db/SupportSQLiteStatement"),
            setOf("execute"),
            setOf("()V"),
        ),
        exact(
            "androidx.support_sqlite_statement.execute_insert",
            setOf("androidx/sqlite/db/SupportSQLiteStatement"),
            setOf("executeInsert"),
            setOf("()J"),
        ),
        exact(
            "androidx.support_sqlite_statement.execute_update_delete",
            setOf("androidx/sqlite/db/SupportSQLiteStatement"),
            setOf("executeUpdateDelete"),
            setOf("()I"),
        ),
        exact(
            "androidx.support_sqlite_statement.query_long",
            setOf("androidx/sqlite/db/SupportSQLiteStatement"),
            setOf("simpleQueryForLong"),
            setOf("()J"),
        ),
        exact(
            "androidx.support_sqlite_statement.query_string",
            setOf("androidx/sqlite/db/SupportSQLiteStatement"),
            setOf("simpleQueryForString"),
            setOf("()Ljava/lang/String;"),
        ),
        exact(
            "android.database.transaction",
            setOf(
                "android/database/sqlite/SQLiteDatabase",
                "androidx/sqlite/db/SupportSQLiteDatabase",
                "androidx/room/RoomDatabase",
            ),
            setOf("beginTransaction", "beginTransactionNonExclusive", "setTransactionSuccessful", "endTransaction"),
            setOf("()V"),
        ),
        exact(
            "android.database.transaction_listener",
            setOf("android/database/sqlite/SQLiteDatabase"),
            setOf("beginTransactionWithListener", "beginTransactionWithListenerNonExclusive"),
            setOf("(Landroid/database/sqlite/SQLiteTransactionListener;)V"),
        ),
        roomAdapter(
            "androidx.room.entity_insert",
            setOf("androidx/room/EntityInsertAdapter", "androidx/room/EntityUpsertAdapter"),
            setOf(
                "insert",
                "insertAndReturnId",
                "insertAndReturnIdsArray",
                "insertAndReturnIdsArrayBox",
                "insertAndReturnIdsList",
                "upsert",
                "upsertAndReturnId",
                "upsertAndReturnIdsArray",
                "upsertAndReturnIdsArrayBox",
                "upsertAndReturnIdsList",
            ),
            setOf("V", "J", "[J", "[Ljava/lang/Long;", "Ljava/util/List;"),
        ),
        roomAdapter(
            "androidx.room.entity_mutation",
            setOf("androidx/room/EntityDeleteOrUpdateAdapter"),
            setOf("handle", "handleMultiple"),
            setOf("I"),
        ),
    )

    private fun exact(
        id: String,
        owners: Set<String>,
        names: Set<String>,
        descriptors: Set<String>,
    ): VersionedBridgeSignature {
        return VersionedBridgeSignature.exact(
            SignatureSpec(id = id, owners = owners, names = names, descriptors = descriptors),
            HookIntent.DatabaseCall(DatabaseFrameworkKind.SQLITE, DatabaseOperationKind.EXECUTE),
        )
    }

    private fun roomAdapter(
        id: String,
        owners: Set<String>,
        names: Set<String>,
        returnDescriptors: Set<String>,
    ): VersionedBridgeSignature {
        return VersionedBridgeSignature(
            id = id,
            intent = HookIntent.DatabaseCall(DatabaseFrameworkKind.ROOM, DatabaseOperationKind.EXECUTE),
            owners = owners,
            names = names,
            descriptorMatcher = { call ->
                ROOM_ADAPTER_ARGUMENTS.any(call.descriptor::startsWith) &&
                    call.descriptor.substringAfterLast(')') in returnDescriptors
            },
        )
    }

    private val ROOM_ADAPTER_ARGUMENTS = arrayOf(
        "(Landroidx/sqlite/SQLiteConnection;Ljava/lang/Object;)",
        "(Landroidx/sqlite/SQLiteConnection;Ljava/lang/Iterable;)",
        "(Landroidx/sqlite/SQLiteConnection;Ljava/util/Collection;)",
        "(Landroidx/sqlite/SQLiteConnection;[Ljava/lang/Object;)",
    )
}

private object CriticalIOBridge : VersionedInstrumentationBridge {
    override val id: String = "critical.io.bridge.v1"
    override val family: String = "io"
    override val signatures: List<VersionedBridgeSignature> = listOf(
        exactIO(
            "kotlin.file.read_bytes",
            "kotlin/io/FilesKt",
            "readBytes",
            "(Ljava/io/File;)[B",
            CriticalIOCallKind.FILE_READ_BYTES,
        ),
        exactIO(
            "kotlin.file.write_bytes",
            "kotlin/io/FilesKt",
            "writeBytes",
            "(Ljava/io/File;[B)V",
            CriticalIOCallKind.FILE_WRITE_BYTES,
        ),
        exactIO(
            "kotlin.file.append_bytes",
            "kotlin/io/FilesKt",
            "appendBytes",
            "(Ljava/io/File;[B)V",
            CriticalIOCallKind.FILE_APPEND_BYTES,
        ),
        exactIO(
            "java.file_descriptor.sync",
            "java/io/FileDescriptor",
            "sync",
            "()V",
            CriticalIOCallKind.FILE_DESCRIPTOR_SYNC,
        ),
        exactIO(
            "java.file_channel.force",
            "java/nio/channels/FileChannel",
            "force",
            "(Z)V",
            CriticalIOCallKind.FILE_CHANNEL_FORCE,
        ),
    )

    private fun exactIO(
        id: String,
        owner: String,
        name: String,
        descriptor: String,
        kind: CriticalIOCallKind,
    ): VersionedBridgeSignature {
        return VersionedBridgeSignature.exact(
            SignatureSpec(id = id, owner = owner, name = name, descriptor = descriptor),
            HookIntent.CriticalIO(kind),
        )
    }
}

private object OkHttp3Bridge : VersionedInstrumentationBridge {
    override val id: String = "okhttp3.bridge.v3"
    override val family: String = "okhttp"
    override val signatures: List<VersionedBridgeSignature> = listOf(
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpEventListenerFactory,
            HookIntent.WrapOkHttpEventListenerFactory,
        ),
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpEventListener,
            HookIntent.InstallOkHttpEventListener,
        ),
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpBuild,
            HookIntent.InstallOkHttpEventListenerFactory,
        ),
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpNewCall,
            HookIntent.GuardOkHttpNewCall,
        ),
        VersionedBridgeSignature.exact(
            HookSignatureCatalog.okHttpNewWebSocket,
            HookIntent.WrapWebSocketListener,
        ),
    )
}

private object KotlinxCoroutinesBridge : VersionedInstrumentationBridge {
    override val id: String = "kotlinx.coroutines.bridge.v1"
    override val family: String = "coroutines"

    private val coroutineBuilderOwners = setOf(
        "kotlinx/coroutines/BuildersKt",
        "kotlinx/coroutines/CoroutineScopeKt",
        "kotlinx/coroutines/SupervisorKt",
        "kotlinx/coroutines/TimeoutKt",
    )

    private val coroutineBuildersWithTopBlock = setOf(
        "launch",
        "async",
        "runBlocking",
    )

    private val coroutineBuildersWithDefaultBlock = setOf(
        "launch\$default",
        "async\$default",
        "runBlocking\$default",
    )

    private val coroutineSuspendBuilders = setOf(
        "withContext",
        "coroutineScope",
        "supervisorScope",
        "withTimeout",
        "withTimeoutOrNull",
    )

    override val signatures: List<VersionedBridgeSignature> = listOf(
        VersionedBridgeSignature(
            id = "kotlinx.coroutines.builders.top_function2.v1",
            intent = HookIntent.CoroutineBlock(CoroutineBlockKind.TOP_FUNCTION2),
            owners = coroutineBuilderOwners,
            names = coroutineBuildersWithTopBlock,
            descriptorMatcher = { call ->
                call.descriptor.endsWith(
                    "Lkotlin/jvm/functions/Function2;)${returnDescriptor(call.owner, call.name)}",
                )
            },
        ),
        VersionedBridgeSignature(
            id = "kotlinx.coroutines.builders.default_function2.v1",
            intent = HookIntent.CoroutineBlock(CoroutineBlockKind.FUNCTION2_BEFORE_INT_OBJECT),
            owners = coroutineBuilderOwners,
            names = coroutineBuildersWithDefaultBlock,
            descriptorMatcher = { call ->
                call.descriptor.endsWith(defaultCoroutineDescriptorSuffix(call.owner, call.name))
            },
        ),
        VersionedBridgeSignature(
            id = "kotlinx.coroutines.suspend_builders.function2_continuation.v1",
            intent = HookIntent.CoroutineBlock(CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION),
            owners = coroutineBuilderOwners,
            names = coroutineSuspendBuilders,
            descriptorMatcher = { call ->
                call.descriptor.endsWith(SUSPEND_COROUTINE_DESCRIPTOR_SUFFIX)
            },
        ),
    )

    private fun returnDescriptor(owner: String, name: String): String {
        return when {
            owner == "kotlinx/coroutines/BuildersKt" && name.startsWith("launch") -> "Lkotlinx/coroutines/Job;"
            owner == "kotlinx/coroutines/BuildersKt" && name.startsWith("async") -> "Lkotlinx/coroutines/Deferred;"
            owner == "kotlinx/coroutines/BuildersKt" && name.startsWith("runBlocking") -> "Ljava/lang/Object;"
            else -> "Ljava/lang/Object;"
        }
    }

    private fun defaultCoroutineDescriptorSuffix(owner: String, name: String): String {
        return "Lkotlin/jvm/functions/Function2;ILjava/lang/Object;)" +
            returnDescriptor(owner, name.removeSuffix("\$default"))
    }

    private const val SUSPEND_COROUTINE_DESCRIPTOR_SUFFIX =
        "Lkotlin/jvm/functions/Function2;Lkotlin/coroutines/Continuation;)Ljava/lang/Object;"
}

private object AndroidHandlerBridge : VersionedInstrumentationBridge {
    override val id: String = "android.handler.bridge.v1"
    override val family: String = "handler"
    override val signatures: List<VersionedBridgeSignature> =
        HookSignatureCatalog.handlerRunnableSignatures.map(VersionedBridgeSignature::exact) +
            HookSignatureCatalog.handlerRemoveCallbacksSignatures.map(VersionedBridgeSignature::exact) +
            VersionedBridgeSignature.exact(HookSignatureCatalog.handlerRemoveCallbacksAndMessages) +
            VersionedBridgeSignature.exact(HookSignatureCatalog.handlerHasCallbacks) +
            HookSignatureCatalog.handlerMessageSendSignatures.map {
                VersionedBridgeSignature.exact(it, HookIntent.HandlerMessageSend)
            }
}

private object JdkExecutorBridge : VersionedInstrumentationBridge {
    override val id: String = "jdk.executor.bridge.v2"
    override val family: String = "executor"
    override val signatures: List<VersionedBridgeSignature> =
        HookSignatureCatalog.executorRunnableSignatures.map(::executorSignature) +
            HookSignatureCatalog.executorCallableSignatures.map(::executorSignature)

    private fun executorSignature(signature: IntentSignature): VersionedBridgeSignature {
        return if (signature.spec.names == setOf("submit")) {
            // ExecutorService permits covariant Future implementations. Guava's
            // ListeningExecutorService therefore emits ListenableFuture in the call descriptor.
            VersionedBridgeSignature.covariantObjectReturn(signature)
        } else {
            VersionedBridgeSignature.exact(signature)
        }
    }
}

private object AndroidViewInteractionOperationBridge : VersionedInstrumentationBridge {
    override val id: String = "android.view.interaction-operation.bridge.v1"
    override val family: String = "interaction-operation"
    override val signatures: List<VersionedBridgeSignature> = listOf(
        VersionedBridgeSignature(
            id = "android.view.click_listener.v1",
            intent = HookIntent.WrapClickListener,
            owners = setOf("android/view/View"),
            names = setOf("setOnClickListener"),
            roles = mapOf(ArgumentRole.Listener to 0),
            descriptorMatcher = { call ->
                call.descriptor == "(Landroid/view/View\$OnClickListener;)V"
            },
        ),
    )
}

private object AndroidLogSpamBridge : VersionedInstrumentationBridge {
    override val id: String = "android.log.bridge.v1"
    override val family: String = "logspam"
    override val signatures: List<VersionedBridgeSignature> =
        logSpamSignatures(
            owner = "android/util/Log",
            sourcePrefix = "android.util.Log",
            descriptors = androidLogDescriptors,
        )
}

private object TimberLogSpamBridge : VersionedInstrumentationBridge {
    override val id: String = "timber.log.bridge.v1"
    override val family: String = "logspam"
    override val signatures: List<VersionedBridgeSignature> =
        logSpamSignatures(
            owner = "timber/log/Timber",
            sourcePrefix = "Timber",
            descriptors = timberDescriptors,
        ) +
            logSpamSignatures(
                owner = "timber/log/Timber\$Tree",
                sourcePrefix = "Timber.Tree",
                descriptors = timberDescriptors,
            )
}

private fun logSpamSignatures(
    owner: String,
    sourcePrefix: String,
    descriptors: Set<String>,
): List<VersionedBridgeSignature> {
    return listOf(
        "v" to 2,
        "d" to 3,
        "i" to 4,
        "w" to 5,
        "e" to 6,
        "wtf" to 7,
    ).map { (name, level) ->
        val source = "$sourcePrefix.$name"
        VersionedBridgeSignature(
            id = "logspam.$source",
            intent = HookIntent.LogSpam(source, level),
            owners = setOf(owner),
            names = setOf(name),
            descriptorMatcher = { call -> call.descriptor in descriptors },
        )
    }
}

private val androidLogDescriptors = setOf(
    "(Ljava/lang/String;Ljava/lang/String;)I",
    "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/Throwable;)I",
    "(Ljava/lang/String;Ljava/lang/Throwable;)I",
)

private val timberDescriptors = setOf(
    "(Ljava/lang/String;[Ljava/lang/Object;)V",
    "(Ljava/lang/Throwable;Ljava/lang/String;[Ljava/lang/Object;)V",
    "(Ljava/lang/Throwable;)V",
)
