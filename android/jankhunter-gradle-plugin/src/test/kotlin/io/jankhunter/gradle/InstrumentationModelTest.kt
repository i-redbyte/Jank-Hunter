package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationModelTest {
    @Test
    fun signatureSpecMatchesOnlyKnownVariants() {
        val call = methodCall(
            owner = "okhttp3/OkHttpClient\$Builder",
            name = "eventListenerFactory",
            descriptor = "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
        )

        assertTrue(HookSignatureCatalog.okHttpEventListenerFactory.matches(call))
        assertFalse(HookSignatureCatalog.okHttpBuild.matches(call))
        assertEquals(0, HookSignatureCatalog.okHttpEventListenerFactory.roles[ArgumentRole.Listener])
    }

    @Test
    fun resolverMapsExistingHooksToCanonicalIntents() {
        val config = testHookConfig(
            okhttp = true,
            webSockets = true,
            handlers = true,
            executors = true,
            coroutines = true,
            logSpam = true,
        )

        assertIntent(
            HookIntent.WrapOkHttpEventListenerFactory,
            methodCall(
                owner = "okhttp3/OkHttpClient\$Builder",
                name = "eventListenerFactory",
                descriptor = "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
            ),
            config,
        )
        assertIntent(
            HookIntent.InstallOkHttpEventListener,
            methodCall(
                owner = "okhttp3/OkHttpClient\$Builder",
                name = "eventListener",
                descriptor = "(Lokhttp3/EventListener;)Lokhttp3/OkHttpClient\$Builder;",
            ),
            config,
        )
        assertIntent(
            HookIntent.GuardOkHttpNewCall,
            methodCall(
                owner = "okhttp3/OkHttpClient",
                name = "newCall",
                descriptor = "(Lokhttp3/Request;)Lokhttp3/Call;",
            ),
            config,
        )
        assertIntent(
            HookIntent.WrapWebSocketListener,
            methodCall(
                owner = "okhttp3/OkHttpClient",
                name = "newWebSocket",
                descriptor = "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
            ),
            config,
        )
        assertIntent(
            HookIntent.HandlerRunnable(HandlerRunnableKind.RUNNABLE_LONG_DELAY),
            methodCall(
                owner = "android/os/Handler",
                name = "postDelayed",
                descriptor = "(Ljava/lang/Runnable;J)Z",
            ),
            config,
        )
        assertIntent(
            HookIntent.ExecutorCallable(ExecutorCallableKind.SINGLE_CALLABLE),
            methodCall(
                owner = "java/util/concurrent/ExecutorService",
                name = "submit",
                descriptor = "(Ljava/util/concurrent/Callable;)Ljava/util/concurrent/Future;",
            ),
            config,
        )
        assertIntent(
            HookIntent.CoroutineBlock(CoroutineBlockKind.FUNCTION2_BEFORE_CONTINUATION),
            methodCall(
                owner = "kotlinx/coroutines/BuildersKt",
                name = "withContext",
                descriptor = "(Lkotlin/coroutines/CoroutineContext;Lkotlin/jvm/functions/Function2;" +
                    "Lkotlin/coroutines/Continuation;)Ljava/lang/Object;",
            ),
            config,
        )
        assertIntent(
            HookIntent.LogSpam("android.util.Log.d", 3),
            methodCall(
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/String;Ljava/lang/String;)I",
            ),
            config,
        )
    }

    @Test
    fun criticalIoMatchesOnlyReviewedWholeOperationAndSyncCalls() {
        val enabled = testHookConfig(ioTracing = true)
        assertIntent(
            HookIntent.CriticalIO(CriticalIOCallKind.FILE_READ_BYTES),
            methodCall("kotlin/io/FilesKt", "readBytes", "(Ljava/io/File;)[B"),
            enabled,
        )
        assertIntent(
            HookIntent.CriticalIO(CriticalIOCallKind.FILE_DESCRIPTOR_SYNC),
            methodCall("java/io/FileDescriptor", "sync", "()V"),
            enabled,
        )
        assertIntent(
            HookIntent.CriticalIO(CriticalIOCallKind.FILE_CHANNEL_FORCE),
            methodCall("java/nio/channels/FileChannel", "force", "(Z)V"),
            enabled,
        )
        assertTrue(
            HookIntentResolver.resolve(
                methodCall("java/io/InputStream", "read", "([B)I"),
                enabled,
            ) is HookDecision.NotMatched,
        )
        assertTrue(
            HookIntentResolver.resolve(
                methodCall("kotlin/io/FilesKt", "readBytes", "(Ljava/io/File;)[B"),
                testHookConfig(ioTracing = false),
            ) is HookDecision.Disabled,
        )
    }

    @Test
    fun databaseMatchesSQLiteRoomAndSupportSQLiteCallsOnlyWhenEnabled() {
        val enabled = testHookConfig(databaseTracing = true)
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.SQLITE,
                DatabaseOperationKind.QUERY,
                "SELECT * FROM messages WHERE id = ?",
                0,
                DatabaseBoundaryKind.DISPATCH,
            ),
            methodCall(
                "android/database/sqlite/SQLiteDatabase",
                "rawQuery",
                "(Ljava/lang/String;[Ljava/lang/String;)Landroid/database/Cursor;",
                databaseQuery = "SELECT * FROM messages WHERE id = ?",
            ),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.SUPPORT_SQLITE,
                DatabaseOperationKind.EXECUTE,
                resultCapture = DatabaseResultCapture.AFFECTED_ROWS,
                statementAction = DatabaseStatementAction.EXECUTE,
            ),
            methodCall("androidx/sqlite/db/SupportSQLiteStatement", "executeUpdateDelete", "()I"),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.ROOM,
                DatabaseOperationKind.STATEMENT,
                queryArgument = 0,
                statementAction = DatabaseStatementAction.REGISTER,
            ),
            methodCall("androidx/room/RoomDatabase", "compileStatement", "(Ljava/lang/String;)Landroidx/sqlite/db/SupportSQLiteStatement;"),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.ROOM,
                DatabaseOperationKind.INSERT,
                resultCapture = DatabaseResultCapture.INSERT_ROW_ID,
            ),
            methodCall(
                "androidx/room/EntityUpsertAdapter",
                "upsertAndReturnId",
                "(Landroidx/sqlite/SQLiteConnection;Ljava/lang/Object;)J",
            ),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.ROOM,
                DatabaseOperationKind.INSERT,
            ),
            methodCall(
                "androidx/room/EntityInsertAdapter",
                "insert",
                "(Landroidx/sqlite/SQLiteConnection;Ljava/lang/Iterable;)V",
            ),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.ROOM,
                DatabaseOperationKind.INSERT,
            ),
            methodCall(
                "androidx/room/EntityUpsertAdapter",
                "upsertAndReturnIdsList",
                "(Landroidx/sqlite/SQLiteConnection;Ljava/util/Collection;)Ljava/util/List;",
            ),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.ROOM,
                DatabaseOperationKind.EXECUTE,
                resultCapture = DatabaseResultCapture.AFFECTED_ROWS,
            ),
            methodCall(
                "androidx/room/EntityDeleteOrUpdateAdapter",
                "handle",
                "(Landroidx/sqlite/SQLiteConnection;Ljava/lang/Object;)I",
            ),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.ROOM,
                DatabaseOperationKind.EXECUTE,
                resultCapture = DatabaseResultCapture.AFFECTED_ROWS,
            ),
            methodCall(
                "androidx/room/EntityDeleteOrUpdateAdapter",
                "handleMultiple",
                "(Landroidx/sqlite/SQLiteConnection;[Ljava/lang/Object;)I",
            ),
            enabled,
        )
        assertTrue(
            HookIntentResolver.resolve(
                methodCall("android/database/sqlite/SQLiteDatabase", "rawQuery", "(Ljava/lang/String;[Ljava/lang/String;)Landroid/database/Cursor;"),
                testHookConfig(databaseTracing = false),
            ) is HookDecision.Disabled,
        )
        assertTrue(
            HookIntentResolver.resolve(
                methodCall(
                    owner = "androidx/sqlite/db/SupportSQLiteDatabase",
                    name = "query",
                    descriptor = "(Ljava/lang/String;)Landroid/database/Cursor;",
                    caller = CallerMethod("androidx/room/RoomDatabase", "query", "()V"),
                ),
                enabled,
            ) is HookDecision.NotMatched,
        )
    }

    @Test
    fun databaseUsesHierarchyForPreparedResultsAndTransactions() {
        val enabled = testHookConfig(databaseTracing = true)
        val customStatement = setOf(
            "com/example/FastStatement",
            "androidx/sqlite/db/SupportSQLiteStatement",
        )
        assertIntent(
            HookIntent.DatabaseCall(
                DatabaseFrameworkKind.SUPPORT_SQLITE,
                DatabaseOperationKind.EXECUTE,
                resultCapture = DatabaseResultCapture.AFFECTED_ROWS,
                statementAction = DatabaseStatementAction.EXECUTE,
            ),
            methodCall(
                "com/example/FastStatement",
                "executeUpdateDelete",
                "()I",
                ownerHierarchy = customStatement,
            ),
            enabled,
        )

        val customDatabase = setOf(
            "com/example/FastDatabase",
            "androidx/sqlite/db/SupportSQLiteDatabase",
        )
        assertIntent(
            HookIntent.DatabaseTransaction(
                DatabaseFrameworkKind.SUPPORT_SQLITE,
                DatabaseTransactionAction.BEGIN,
                DatabaseTransactionModeKind.IMMEDIATE,
            ),
            methodCall(
                "com/example/FastDatabase",
                "beginTransactionNonExclusive",
                "()V",
                ownerHierarchy = customDatabase,
            ),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseTransaction(
                DatabaseFrameworkKind.SQLITE,
                DatabaseTransactionAction.BEGIN,
                DatabaseTransactionModeKind.IMMEDIATE,
            ),
            methodCall(
                "android/database/sqlite/SQLiteDatabase",
                "beginTransactionWithListenerNonExclusive",
                "(Landroid/database/sqlite/SQLiteTransactionListener;)V",
            ),
            enabled,
        )
        assertIntent(
            HookIntent.DatabaseTransaction(
                DatabaseFrameworkKind.SUPPORT_SQLITE,
                DatabaseTransactionAction.END,
                DatabaseTransactionModeKind.UNKNOWN,
            ),
            methodCall(
                "com/example/FastDatabase",
                "endTransaction",
                "()V",
                ownerHierarchy = customDatabase,
            ),
            enabled,
        )
        assertTrue(
            HookIntentResolver.resolve(
                methodCall(
                    "androidx/sqlite/db/SupportSQLiteStatement",
                    "executeUpdateDelete",
                    "()J",
                ),
                enabled,
            ) is HookDecision.Unsupported,
        )
    }

    @Test
    fun roomMutationOwnersMatchPublishedRoomRuntimeClasses() {
        val adapter = Class.forName("androidx.room.EntityDeleteOrUpdateAdapter")
        val connection = Class.forName("androidx.sqlite.SQLiteConnection")
        adapter.getDeclaredMethod("handle", connection, Any::class.java)
        val owner = adapter.name.replace('.', '/')
        val decision = HookIntentResolver.resolve(
            methodCall(owner, "handle", "(Landroidx/sqlite/SQLiteConnection;Ljava/lang/Object;)I"),
            testHookConfig(databaseTracing = true),
        )
        assertTrue("Published Room mutation adapter is not instrumented", decision is HookDecision.Matched)
    }

    @Test
    fun executorSubmitAcceptsGuavaCovariantFutureReturn() {
        val decision = HookIntentResolver.resolve(
            methodCall(
                owner = "com/google/common/util/concurrent/ListeningExecutorService",
                ownerHierarchy = setOf(
                    "com/google/common/util/concurrent/ListeningExecutorService",
                    "java/util/concurrent/ExecutorService",
                ),
                name = "submit",
                descriptor = "(Ljava/lang/Runnable;)" +
                    "Lcom/google/common/util/concurrent/ListenableFuture;",
            ),
            testHookConfig(executors = true),
        )

        require(decision is HookDecision.Matched)
        assertEquals(HookIntent.ExecutorRunnable(ExecutorRunnableKind.SINGLE_RUNNABLE), decision.intent)
        assertEquals("jdk.executor.bridge.v2", decision.bridgeId)
        assertEquals("jdk.executor.submit.runnable", decision.signatureId)
    }

    @Test
    fun resolverReportsVersionedBridgeForAllHookFamilies() {
        val okHttpDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "okhttp3/OkHttpClient\$Builder",
                name = "build",
                descriptor = "()Lokhttp3/OkHttpClient;",
            ),
            testHookConfig(okhttp = true),
        )
        require(okHttpDecision is HookDecision.Matched)
        assertEquals("okhttp3.bridge.v3", okHttpDecision.bridgeId)
        assertEquals("okhttp3.builder.build.v3", okHttpDecision.signatureId)

        val handlerDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/os/Handler",
                name = "post",
                descriptor = "(Ljava/lang/Runnable;)Z",
            ),
            testHookConfig(handlers = true),
        )
        require(handlerDecision is HookDecision.Matched)
        assertEquals("android.handler.bridge.v1", handlerDecision.bridgeId)

        val executorDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "java/util/concurrent/Executor",
                name = "execute",
                descriptor = "(Ljava/lang/Runnable;)V",
            ),
            testHookConfig(executors = true),
        )
        require(executorDecision is HookDecision.Matched)
        assertEquals("jdk.executor.bridge.v2", executorDecision.bridgeId)

        val coroutineDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "kotlinx/coroutines/BuildersKt",
                name = "launch\$default",
                descriptor = "(Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                    "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;ILjava/lang/Object;)" +
                    "Lkotlinx/coroutines/Job;",
            ),
            testHookConfig(coroutines = true),
        )
        require(coroutineDecision is HookDecision.Matched)
        assertEquals("kotlinx.coroutines.bridge.v1", coroutineDecision.bridgeId)
        assertEquals("kotlinx.coroutines.builders.default_function2.v1", coroutineDecision.signatureId)

        val interactionDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/view/View",
                name = "setOnClickListener",
                descriptor = "(Landroid/view/View\$OnClickListener;)V",
            ),
            testHookConfig(interactionOperations = true),
        )
        require(interactionDecision is HookDecision.Matched)
        assertEquals("android.view.interaction-operation.bridge.v1", interactionDecision.bridgeId)

        val logSpamDecision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/String;Ljava/lang/String;)I",
            ),
            testHookConfig(logSpam = true),
        )
        require(logSpamDecision is HookDecision.Matched)
        assertEquals("android.log.bridge.v1", logSpamDecision.bridgeId)
    }

    @Test
    fun moduleRegistryExposesExtensibleFamilies() {
        val bridgeIds = VersionedBridgeCatalog.all().map { it.id }.toSet()
        val families = VersionedBridgeCatalog.all().map { it.family }.toSet()
        val moduleIds = HookIntentResolver.modules().map { it.id }.toSet()
        val providerBridgeIds = DefaultInstrumentationBridgeProvider.bridges().map { it.id }.toSet()

        assertTrue("okhttp3.bridge.v3" in bridgeIds)
        assertTrue("android.handler.bridge.v1" in bridgeIds)
        assertTrue("jdk.executor.bridge.v2" in bridgeIds)
        assertTrue("kotlinx.coroutines.bridge.v1" in bridgeIds)
        assertTrue("android.view.interaction-operation.bridge.v1" in bridgeIds)
        assertTrue("android.log.bridge.v1" in bridgeIds)
        assertTrue("critical.io.bridge.v1" in bridgeIds)
        assertEquals(providerBridgeIds, bridgeIds)
        assertTrue("okhttp" in families)
        assertTrue("handler" in families)
        assertTrue("executor" in families)
        assertTrue("coroutines" in families)
        assertTrue("interaction-operation" in families)
        assertTrue("logspam" in families)
        assertTrue("io" in families)
        assertEquals(
            setOf(
                "okhttp",
                "websocket",
                "handler",
                "executor",
                "coroutine",
                "interaction-operation",
                "logspam",
                "critical_io",
                "database",
            ),
            moduleIds,
        )
        assertTrue(
            VersionedBridgeCatalog.all()
                .flatMap { it.signatures }
                .any { it.id == "kotlinx.coroutines.suspend_builders.function2_continuation.v1" },
        )
    }

    @Test
    fun onlyDatabaseInstrumentationRequiresControlFlow() {
        assertTrue(HookIntentResolver.needsControlFlow())
        assertFalse(HookIntentResolver.needsControlFlow(testHookConfig(okhttp = true, coroutines = true)))
        assertTrue(HookIntentResolver.needsControlFlow(testHookConfig(databaseTracing = true)))
        assertEquals(setOf("database"), HookIntentResolver.modules().filter { it.needsControlFlow }.map { it.id }.toSet())
    }

    @Test
    fun stableMethodIdsUseCanonicalNullSeparatedFNV64() {
        val first = OwnerIds.methodId("com/example/Foo", "load", "()V")
        val second = OwnerIds.methodId("com.example.Foo", "load", "()V")
        val differentDescriptor = OwnerIds.methodId("com/example/Foo", "load", "(I)V")

        assertEquals(first, second)
        assertEquals("stable:0x865bbc6d7b314f77", OwnerIds.canonical(first))
        assertEquals("com.example.Foo.load", OwnerIds.readableOwner("com/example/Foo", "load"))
        assertFalse(first == differentDescriptor)
    }

    @Test
    fun resolverMatchesKnownApiThroughReceiverHierarchy() {
        val call = methodCall(
            owner = "com/example/TracingHandler",
            name = "post",
            descriptor = "(Ljava/lang/Runnable;)Z",
            ownerHierarchy = setOf("com/example/TracingHandler", "android/os/Handler"),
        )

        assertIntent(
            HookIntent.HandlerRunnable(HandlerRunnableKind.SINGLE_RUNNABLE),
            call,
            testHookConfig(handlers = true),
        )
    }

    @Test
    fun resolverRespectsDisabledGates() {
        val call = methodCall(
            owner = "android/os/Handler",
            name = "post",
            descriptor = "(Ljava/lang/Runnable;)Z",
        )

        val decision = HookIntentResolver.resolve(call, testHookConfig())
        require(decision is HookDecision.Disabled)
        assertEquals("handler", decision.moduleId)
        assertEquals("disabled_by_gate", decision.reason)
    }

    @Test
    fun resolverKeepsWebSocketGateSeparateFromOkHttpGate() {
        val call = methodCall(
            owner = "okhttp3/OkHttpClient",
            name = "newWebSocket",
            descriptor = "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
        )

        val disabled = HookIntentResolver.resolve(call, testHookConfig(okhttp = true))
        require(disabled is HookDecision.Disabled)
        assertEquals("websocket", disabled.moduleId)
        assertIntent(HookIntent.WrapWebSocketListener, call, testHookConfig(webSockets = true))
    }

    @Test
    fun resolverReportsUnsupportedKnownOwnerSignature() {
        val decision = HookIntentResolver.resolve(
            methodCall(
                owner = "android/os/Handler",
                name = "post",
                descriptor = "(Ljava/lang/Runnable;Ljava/lang/Object;)Z",
            ),
            testHookConfig(handlers = true),
        )

        require(decision is HookDecision.Unsupported)
        assertEquals("handler", decision.moduleId)
        assertEquals("unsupported_signature", decision.reason)
    }

    @Test
    fun logSpamBridgeRejectsUnknownDescriptors() {
        val supported = HookIntentResolver.resolve(
            methodCall(
                owner = "android/util/Log",
                name = "w",
                descriptor = "(Ljava/lang/String;Ljava/lang/Throwable;)I",
            ),
            testHookConfig(logSpam = true),
        )
        require(supported is HookDecision.Matched)
        assertEquals("android.log.bridge.v1", supported.bridgeId)

        val unsupported = HookIntentResolver.resolve(
            methodCall(
                owner = "android/util/Log",
                name = "d",
                descriptor = "(Ljava/lang/Object;)V",
            ),
            testHookConfig(logSpam = true),
        )
        require(unsupported is HookDecision.Unsupported)
        assertEquals("logspam", unsupported.moduleId)
    }

    private fun assertIntent(expected: HookIntent, call: MethodCall, config: HookConfig) {
        val decision = HookIntentResolver.resolve(call, config)
        require(decision is HookDecision.Matched) { "Expected matched hook for $call but got $decision" }
        assertEquals(expected, decision.intent)
        assertTrue(decision.signatureId.isNotBlank())
    }

    private fun methodCall(
        owner: String,
        name: String,
        descriptor: String,
        ownerHierarchy: Set<String> = setOf(owner),
        caller: CallerMethod? = null,
        databaseQuery: String? = null,
    ): MethodCall {
        return MethodCall(
            owner = owner,
            name = name,
            descriptor = descriptor,
            caller = caller,
            ownerHierarchy = ownerHierarchy,
            databaseQuery = databaseQuery,
        )
    }

    private fun testHookConfig(
        methodCounters: Boolean = false,
        okhttp: Boolean = false,
        webSockets: Boolean = false,
        handlers: Boolean = false,
        executors: Boolean = false,
        coroutines: Boolean = false,
        interactionOperations: Boolean = false,
        logSpam: Boolean = false,
        classGraph: Boolean = false,
        runtimeCallGraph: Boolean = false,
        ioTracing: Boolean = false,
        databaseTracing: Boolean = false,
    ): HookConfig {
        return HookConfig(
            methodCounters = methodCounters,
            okhttp = okhttp,
            webSockets = webSockets,
            handlers = handlers,
            executors = executors,
            coroutines = coroutines,
            interactionOperations = interactionOperations,
            logSpam = logSpam,
            classGraph = classGraph,
            runtimeCallGraph = runtimeCallGraph,
            ioTracing = ioTracing,
            databaseTracing = databaseTracing,
            classGraphDirectory = "",
            instrumentationDiagnosticsDirectory = "",
        )
    }
}
