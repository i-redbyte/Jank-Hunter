package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.analysis.Analyzer
import org.objectweb.asm.tree.analysis.BasicVerifier
import org.objectweb.asm.util.CheckClassAdapter

class InstrumentationSmokeTest {
    @Test
    fun instrumentsMixedAndroidSdkCallSitesIntoVerifiableBytecode() {
        val instrumented = instrument(mixedHookFixture(), okHttpHelperAvailable = true)
        val verifierDiagnostics = java.io.StringWriter()
        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(verifierDiagnostics))
        assertTrue(
            "ASM verifier rejected instrumented bytecode:\n$verifierDiagnostics",
            verifierDiagnostics.toString().isBlank(),
        )
        val calls = collectCalls(instrumented)

        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "startAnnotatedOperation")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "finishAnnotatedOperation")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapHandlerRunnable")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "onHandlerPostResult")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "handlerWrappers")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "clearHandlerWrappers")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapRunnable")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapCoroutineBlock")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapClickListener")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject")))
        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "recordLogSpam")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "wrapEventListenerFactory")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "installEventListener")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "installEventListenerFactory")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "newCall")))
        assertTrue(calls.contains(Call("io/jankhunter/okhttp3/JankHunterOkHttp3", "wrapWebSocketListener")))
        assertEquals(0, countMethodCalls(instrumented, "exercise", "okhttp3/OkHttpClient\$Builder", "eventListener"))
        assertEquals(0, countMethodCalls(instrumented, "exercise", "okhttp3/OkHttpClient", "newCall"))
        assertTrue(calls.contains(Call("android/os/Handler", "post")))
        assertEquals(1, countMethodCalls(instrumented, "exercise", "android/os/Handler", "post"))
        assertEquals(
            2,
            countMethodCalls(
                instrumented,
                "exercise",
                "io/jankhunter/runtime/JankHunterHooks",
                "onHandlerPostResult",
            ),
        )
        assertTrue(
            methodCalls(instrumented, "onViewRecycled")
                .none { it == Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject") },
        )
        assertTrue(
            methodCalls(instrumented, "onCleared").contains(
                Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject"),
            ),
        )
    }

    @Test
    fun replacesReviewedIoCallsWithoutTouchingLowLevelReads() {
        val instrumented = instrument(criticalIOFixture())
        val verifierDiagnostics = java.io.StringWriter()
        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(verifierDiagnostics))
        assertTrue(verifierDiagnostics.toString(), verifierDiagnostics.toString().isBlank())

        assertEquals(1, countMethodCalls(instrumented, "io", "io/jankhunter/runtime/JankHunterIOHooks", "readFileBytes"))
        assertEquals(1, countMethodCalls(instrumented, "io", "io/jankhunter/runtime/JankHunterIOHooks", "writeFileBytes"))
        assertEquals(1, countMethodCalls(instrumented, "io", "io/jankhunter/runtime/JankHunterIOHooks", "appendFileBytes"))
        assertEquals(1, countMethodCalls(instrumented, "io", "io/jankhunter/runtime/JankHunterIOHooks", "syncFileDescriptor"))
        assertEquals(1, countMethodCalls(instrumented, "io", "io/jankhunter/runtime/JankHunterIOHooks", "forceFileChannel"))
        assertEquals(1, countMethodCalls(instrumented, "io", "java/io/InputStream", "read"))
        assertEquals(0, countMethodCalls(instrumented, "io", "kotlin/io/FilesKt", "readBytes"))
        assertEquals(0, countMethodCalls(instrumented, "io", "java/io/FileDescriptor", "sync"))
    }

    @Test
    fun databaseCallIsMeasuredOnSuccessAndFailureWithBuildTimeNormalizedSql() {
        val instrumented = instrument(databaseFixture())
        val verifierDiagnostics = java.io.StringWriter()
        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(verifierDiagnostics))
        assertTrue(verifierDiagnostics.toString(), verifierDiagnostics.toString().isBlank())

        assertEquals(1, countMethodCalls(instrumented, "database", "android/database/sqlite/SQLiteDatabase", "rawQuery"))
        assertEquals(1, countMethodCalls(instrumented, "database", "io/jankhunter/runtime/JankHunterHooks", "enterDatabase"))
        assertEquals(2, countMethodCalls(instrumented, "database", "io/jankhunter/runtime/JankHunterHooks", "exitDatabase"))
        assertEquals(0, countMethodCalls(instrumented, "database", "io/jankhunter/runtime/JankHunterHooks", "normalizeDatabaseQuery"))
        assertEquals(0, countMethodCalls(instrumented, "database", "io/jankhunter/runtime/JankHunterHooks", "databaseStatementFingerprint"))
        val constants = stringConstants(instrumented, "database")
        assertTrue(constants.contains("SELECT * FROM messages WHERE id = 42 AND author = 'secret-author'"))
        assertTrue(constants.contains("SELECT * FROM messages WHERE id = ? AND author = ?"))
    }

    @Test
    fun databaseSqlComesFromCurrentInvocationArgumentNotStaleLdc() {
        val instrumented = instrument(dynamicDatabaseFixture())
        val verifierDiagnostics = java.io.StringWriter()
        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(verifierDiagnostics))
        assertTrue(verifierDiagnostics.toString(), verifierDiagnostics.toString().isBlank())

        assertEquals(1, countMethodCalls(instrumented, "databaseDynamic", "io/jankhunter/runtime/JankHunterHooks", "normalizeDatabaseQuery"))
        assertEquals(1, countMethodCalls(instrumented, "databaseDynamic", "io/jankhunter/runtime/JankHunterHooks", "databaseStatementFingerprint"))
        assertFalse(
            stringConstants(instrumented, "databaseDynamic")
                .contains("SELECT * FROM stale_private_table WHERE token=?"),
        )
    }

    @Test
    fun preparedStatementCompileExecuteAndNaturalResultAreLinkedWithoutConsumingReturnValue() {
        val instrumented = instrument(preparedDatabaseFixture())
        verifyWithoutLoadingAndroidTypes(instrumented)

        assertEquals(
            1,
            countMethodCalls(
                instrumented,
                "preparedDatabase",
                "io/jankhunter/runtime/JankHunterHooks",
                "registerPreparedStatement",
            ),
        )
        assertEquals(
            1,
            countMethodCalls(
                instrumented,
                "preparedDatabase",
                "io/jankhunter/runtime/JankHunterHooks",
                "resolvePreparedStatement",
            ),
        )
        assertEquals(
            1,
            countMethodCalls(
                instrumented,
                "preparedDatabase",
                "io/jankhunter/runtime/JankHunterHooks",
                "databaseResultCountBucket",
            ),
        )
        assertEquals(
            1,
            countMethodCalls(
                instrumented,
                "preparedDatabase",
                "io/jankhunter/runtime/JankHunterHooks",
                "enterDatabase",
            ),
        )
        assertEquals(
            2,
            countMethodCalls(
                instrumented,
                "preparedDatabase",
                "io/jankhunter/runtime/JankHunterHooks",
                "exitDatabase",
            ),
        )
    }

    @Test
    fun databaseFailureHookPrecedesUserCatchHandler() {
        val instrumented = instrument(caughtDatabaseFailureFixture())
        verifyWithoutLoadingAndroidTypes(instrumented)

        val classNode = ClassNode()
        ClassReader(instrumented).accept(classNode, 0)
        val method = classNode.methods.single { it.name == "caughtDatabaseFailure" }
        val userHandlerIndex = method.tryCatchBlocks.indexOfFirst {
            it.type == "android/database/sqlite/SQLiteConstraintException"
        }
        val databaseHandlerIndex = method.tryCatchBlocks.indexOfFirst { block ->
            block.type == null && generateSequence(block.handler.next) { it.next }
                .take(32)
                .filterIsInstance<org.objectweb.asm.tree.MethodInsnNode>()
                .any { it.owner == "io/jankhunter/runtime/JankHunterHooks" && it.name == "exitDatabase" }
        }
        assertTrue("database handler missing: ${method.tryCatchBlocks}", databaseHandlerIndex >= 0)
        assertTrue(
            "database handler must observe the failure before the user handler: ${method.tryCatchBlocks}",
            databaseHandlerIndex < userHandlerIndex,
        )
    }

    @Test
    fun transactionLifecycleHooksAreVerifiableAndCloseOnEndFailure() {
        val instrumented = instrument(databaseTransactionFixture())
        verifyWithoutLoadingAndroidTypes(instrumented)

        assertEquals(1, countMethodCalls(instrumented, "transaction", "io/jankhunter/runtime/JankHunterHooks", "beginDatabaseTransaction"))
        assertEquals(
            1,
            countMethodCalls(
                instrumented,
                "transaction",
                "io/jankhunter/runtime/JankHunterHooks",
                "markDatabaseTransactionSuccessful",
            ),
        )
        assertEquals(2, countMethodCalls(instrumented, "transaction", "io/jankhunter/runtime/JankHunterHooks", "endDatabaseTransaction"))
    }

    @Test
    fun generatedRoomSqlLambdaMeasuresWholeQueryAndNormalizesRuntimeArgument() {
        val instrumented = instrument(generatedRoomSqlLambdaFixture())
        val verifierDiagnostics = java.io.StringWriter()
        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(verifierDiagnostics))
        assertTrue(verifierDiagnostics.toString(), verifierDiagnostics.toString().isBlank())

        val method = "loadMessages\$lambda\$0"
        assertEquals(1, countMethodCalls(instrumented, method, "io/jankhunter/runtime/JankHunterHooks", "normalizeDatabaseQuery"))
        assertEquals(1, countMethodCalls(instrumented, method, "io/jankhunter/runtime/JankHunterHooks", "databaseQueryOperation"))
        assertEquals(1, countMethodCalls(instrumented, method, "io/jankhunter/runtime/JankHunterHooks", "enterDatabase"))
        assertEquals(2, countMethodCalls(instrumented, method, "io/jankhunter/runtime/JankHunterHooks", "exitDatabase"))
    }

    @Test
    fun matchedOkHttpHookWithoutRuntimeHelperFailsBeforeEmittingCrashableBytecode() {
        val error = assertThrows(GradleException::class.java) {
            instrument(mixedHookFixture(), okHttpHelperAvailable = false)
        }

        val message = error.message.orEmpty()
        assertTrue(message.contains("example.AsmSmoke#exercise"))
        assertTrue(message.contains("line 73"))
        assertTrue(message.contains("okhttp3.OkHttpClient\$Builder.eventListenerFactory"))
        assertTrue(message.contains("jankhunter-okhttp3"))
        assertTrue(message.contains("Instrumentation stopped before emitting bytecode"))
    }

    @Test
    fun missingRuntimeHelperDoesNotFailWithoutMatchedNetworkCallSite() {
        val instrumented = instrument(nonNetworkFixture(), okHttpHelperAvailable = false)
        val calls = collectCalls(instrumented)

        assertTrue(calls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "recordMethodCall")))
        assertTrue(calls.none { call -> call.owner == "io/jankhunter/okhttp3/JankHunterOkHttp3" })
    }

    @Test
    fun matchedWebSocketHookWithoutRuntimeHelperAlsoFails() {
        val error = assertThrows(GradleException::class.java) {
            instrument(webSocketFixture(), okHttpHelperAvailable = false)
        }

        val message = error.message.orEmpty()
        assertTrue(message.contains("newWebSocket"))
        assertTrue(message.contains("line 91"))
    }

    @Test
    fun instrumentationMarkerMakesRepeatedTransformIdempotent() {
        val once = instrument(mixedHookFixture())
        val twice = instrument(once)

        assertEquals(
            countMethodCalls(once, "exercise", "io/jankhunter/runtime/JankHunterHooks", "wrapRunnable"),
            countMethodCalls(twice, "exercise", "io/jankhunter/runtime/JankHunterHooks", "wrapRunnable"),
        )
        assertEquals(1, countClassAnnotation(twice, InstrumentationMarker.DESCRIPTOR))
    }

    @Test
    fun lifecycleHooksRequireSupportedAndroidHierarchy() {
        val instrumented = instrument(
            mixedHookFixture(),
            classHierarchy = setOf("example/AsmSmoke", "java/lang/Object"),
        )

        assertTrue(
            methodCalls(instrumented, "onViewRecycled")
                .none { it == Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject") },
        )
    }

    @Test
    fun appliedCallSiteHooksEmbedReadableOwner() {
        val instrumented = instrument(
            mixedHookFixture(),
            methodCounters = false,
            runtimeCallGraph = false,
        )

        assertTrue(instrumented.toString(Charsets.ISO_8859_1).contains("example.AsmSmoke.exercise"))
    }

    @Test
    fun constructorsInstrumentBoundariesAndCallSitesAfterSuperWhileClassInitializersStayUntouched() {
        val instrumented = instrument(mixedHookFixture())
        val constructorCalls = methodCalls(instrumented, "<init>")
        val classInitializerCalls = methodCalls(instrumented, "<clinit>")

        assertTrue(constructorCalls.contains(Call("io/jankhunter/runtime/JankHunterHooks", "wrapHandlerRunnable")))
        assertEquals(1, countMethodCalls(instrumented, "<init>", "android/os/Handler", "post"))
        setOf("recordMethodCall", "enterMethod", "exitMethod", "startAnnotatedOperation", "finishAnnotatedOperation")
            .forEach { expected ->
                assertTrue(constructorCalls.contains(Call("io/jankhunter/runtime/JankHunterHooks", expected)))
            }
        assertTrue(
            constructorCalls.none {
                it == Call("io/jankhunter/runtime/JankHunterHooks", "watchLifecycleObject")
            },
        )
        assertTrue(classInitializerCalls.none { it.owner.startsWith("io/jankhunter/") })
        assertTrue(classInitializerCalls.contains(Call("android/util/Log", "d")))
    }

    @Test
    fun constructorBoundariesSurviveKotlinStylePostSuperExceptionHandlers() {
        val instrumented = instrument(kotlinStyleConstructorWithExceptionHandlerFixture())
        val verifierDiagnostics = java.io.StringWriter()

        CheckClassAdapter.verify(ClassReader(instrumented), false, java.io.PrintWriter(verifierDiagnostics))

        assertTrue(
            "ASM verifier rejected instrumented constructor bytecode:\n$verifierDiagnostics",
            verifierDiagnostics.toString().isBlank(),
        )
        assertEquals(
            1,
            countMethodCalls(instrumented, "<init>", "io/jankhunter/runtime/JankHunterHooks", "enterMethod"),
        )
        assertEquals(
            2,
            countMethodCalls(instrumented, "<init>", "io/jankhunter/runtime/JankHunterHooks", "exitMethod"),
        )
        assertEquals(
            1,
            countMethodCalls(
                instrumented,
                "<init>",
                "io/jankhunter/runtime/JankHunterHooks",
                "startAnnotatedOperation",
            ),
        )
        assertEquals(
            2,
            countMethodCalls(
                instrumented,
                "<init>",
                "io/jankhunter/runtime/JankHunterHooks",
                "finishAnnotatedOperation",
            ),
        )
    }

    private fun instrument(
        bytes: ByteArray,
        classHierarchy: Set<String> = setOf(
            "example/AsmSmoke",
            "androidx/lifecycle/ViewModel",
            "androidx/recyclerview/widget/RecyclerView\$Adapter",
        ),
        methodCounters: Boolean = true,
        runtimeCallGraph: Boolean = true,
        okHttpHelperAvailable: Boolean = true,
    ): ByteArray {
        val reader = ClassReader(bytes)
        val writer = SafeClassWriter(reader, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        reader.accept(
            JankHunterClassVisitor(
                writer,
                "example/AsmSmoke",
                HookConfig(
                    methodCounters = methodCounters,
                    okhttp = true,
                    webSockets = true,
                    okHttpHelperAvailable = okHttpHelperAvailable,
                    handlers = true,
                    executors = true,
                    coroutines = true,
                    interactionOperations = true,
                    logSpam = true,
                    classGraph = true,
                    runtimeCallGraph = runtimeCallGraph,
                    classGraphDirectory = "",
                    instrumentationDiagnosticsDirectory = "",
                    lifecycleLeaks = true,
                    ioTracing = true,
                ),
                classHierarchy = classHierarchy,
            ),
            ClassReader.EXPAND_FRAMES,
        )
        return writer.toByteArray()
    }

    private fun mixedHookFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC,
            "<init>",
            "(Landroid/os/Handler;Ljava/lang/Runnable;)V",
            null,
            null,
        ).apply {
            visitAnnotation(OPERATION_DESCRIPTOR, false).finishStringValue("constructor")
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitVarInsn(Opcodes.ALOAD, 2)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "post", "(Ljava/lang/Runnable;)Z", false)
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_STATIC, "<clinit>", "()V", null, null).apply {
            visitCode()
            visitLdcInsn("JankHunter")
            visitLdcInsn("class init")
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "android/util/Log",
                "d",
                "(Ljava/lang/String;Ljava/lang/String;)I",
                false,
            )
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC, "exercise", MIXED_DESCRIPTOR, null, null).apply {
            visitAnnotation(OPERATION_DESCRIPTOR, false).finishStringValue("asmSmoke")
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "post", "(Ljava/lang/Runnable;)Z", false)
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "removeCallbacks", "(Ljava/lang/Runnable;)V", false)

            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "android/os/Handler", "hasCallbacks", "(Ljava/lang/Runnable;)Z", false)
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 0)
            visitInsn(Opcodes.ACONST_NULL)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/os/Handler",
                "removeCallbacksAndMessages",
                "(Ljava/lang/Object;)V",
                false,
            )

            visitVarInsn(Opcodes.ALOAD, 2)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEINTERFACE, "java/util/concurrent/Executor", "execute", "(Ljava/lang/Runnable;)V", true)

            visitVarInsn(Opcodes.ALOAD, 3)
            visitVarInsn(Opcodes.ALOAD, 4)
            visitVarInsn(Opcodes.ALOAD, 5)
            visitVarInsn(Opcodes.ALOAD, 6)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlinx/coroutines/BuildersKt",
                "launch",
                "(Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                    "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;)Lkotlinx/coroutines/Job;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 7)
            visitVarInsn(Opcodes.ALOAD, 8)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/view/View",
                "setOnClickListener",
                "(Landroid/view/View\$OnClickListener;)V",
                false,
            )

            visitVarInsn(Opcodes.ALOAD, 9)
            visitVarInsn(Opcodes.ALOAD, 13)
            val okHttpLine = Label()
            visitLabel(okHttpLine)
            visitLineNumber(73, okHttpLine)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient\$Builder",
                "eventListenerFactory",
                "(Lokhttp3/EventListener\$Factory;)Lokhttp3/OkHttpClient\$Builder;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 9)
            visitVarInsn(Opcodes.ALOAD, 14)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient\$Builder",
                "eventListener",
                "(Lokhttp3/EventListener;)Lokhttp3/OkHttpClient\$Builder;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 9)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient\$Builder",
                "build",
                "()Lokhttp3/OkHttpClient;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 10)
            visitVarInsn(Opcodes.ALOAD, 11)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient",
                "newCall",
                "(Lokhttp3/Request;)Lokhttp3/Call;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitVarInsn(Opcodes.ALOAD, 10)
            visitVarInsn(Opcodes.ALOAD, 11)
            visitVarInsn(Opcodes.ALOAD, 12)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient",
                "newWebSocket",
                "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
                false,
            )
            visitInsn(Opcodes.POP)

            visitLdcInsn("JankHunter")
            visitLdcInsn("asm smoke")
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "android/util/Log",
                "d",
                "(Ljava/lang/String;Ljava/lang/String;)I",
                false,
            )
            visitInsn(Opcodes.POP)

            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PROTECTED, "onCleared", "()V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitMethod(Opcodes.ACC_PUBLIC, "onViewRecycled", "(Ljava/lang/Object;)V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun nonNetworkFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "work", "()V", null, null).apply {
            visitCode()
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun criticalIOFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "io",
            "(Ljava/io/File;[BLjava/io/FileDescriptor;Ljava/nio/channels/FileChannel;Ljava/io/InputStream;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESTATIC, "kotlin/io/FilesKt", "readBytes", "(Ljava/io/File;)[B", false)
            visitInsn(Opcodes.POP)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKESTATIC, "kotlin/io/FilesKt", "writeBytes", "(Ljava/io/File;[B)V", false)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKESTATIC, "kotlin/io/FilesKt", "appendBytes", "(Ljava/io/File;[B)V", false)
            visitVarInsn(Opcodes.ALOAD, 2)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/io/FileDescriptor", "sync", "()V", false)
            visitVarInsn(Opcodes.ALOAD, 3)
            visitInsn(Opcodes.ICONST_1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/nio/channels/FileChannel", "force", "(Z)V", false)
            visitVarInsn(Opcodes.ALOAD, 4)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(Opcodes.INVOKEVIRTUAL, "java/io/InputStream", "read", "([B)I", false)
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun databaseFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "database",
            "(Landroid/database/sqlite/SQLiteDatabase;[Ljava/lang/String;)Landroid/database/Cursor;",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitLdcInsn("SELECT * FROM messages WHERE id = 42 AND author = 'secret-author'")
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/database/sqlite/SQLiteDatabase",
                "rawQuery",
                "(Ljava/lang/String;[Ljava/lang/String;)Landroid/database/Cursor;",
                false,
            )
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun dynamicDatabaseFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "databaseDynamic",
            "(Landroid/database/sqlite/SQLiteDatabase;Ljava/lang/String;[Ljava/lang/String;)Landroid/database/Cursor;",
            null,
            null,
        ).apply {
            visitCode()
            visitLdcInsn("SELECT * FROM stale_private_table WHERE token='do-not-attribute'")
            visitInsn(Opcodes.POP)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitVarInsn(Opcodes.ALOAD, 2)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/database/sqlite/SQLiteDatabase",
                "rawQuery",
                "(Ljava/lang/String;[Ljava/lang/String;)Landroid/database/Cursor;",
                false,
            )
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun generatedRoomSqlLambdaFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitField(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_FINAL,
            "__db",
            "Landroidx/room/RoomDatabase;",
            null,
            null,
        ).visitEnd()
        writer.visitMethod(
            Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC or Opcodes.ACC_FINAL,
            "loadMessages\$lambda\$0",
            "(Ljava/lang/String;Landroidx/sqlite/SQLiteConnection;)Ljava/util/List;",
            null,
            null,
        ).apply {
            visitCode()
            visitTypeInsn(Opcodes.NEW, "java/util/ArrayList")
            visitInsn(Opcodes.DUP)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/util/ArrayList", "<init>", "()V", false)
            visitInsn(Opcodes.ARETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun preparedDatabaseFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/PreparedSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "preparedDatabase",
            "(Landroid/database/sqlite/SQLiteDatabase;Ljava/lang/String;)I",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/database/sqlite/SQLiteDatabase",
                "compileStatement",
                "(Ljava/lang/String;)Landroid/database/sqlite/SQLiteStatement;",
                false,
            )
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/database/sqlite/SQLiteStatement",
                "executeUpdateDelete",
                "()I",
                false,
            )
            visitInsn(Opcodes.IRETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun caughtDatabaseFailureFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/CaughtDatabaseFailure", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "caughtDatabaseFailure",
            "(Landroidx/sqlite/db/SupportSQLiteStatement;)Z",
            null,
            null,
        ).apply {
            val tryStart = Label()
            val tryEnd = Label()
            val userHandler = Label()
            visitTryCatchBlock(
                tryStart,
                tryEnd,
                userHandler,
                "android/database/sqlite/SQLiteConstraintException",
            )
            visitCode()
            visitLabel(tryStart)
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(
                Opcodes.INVOKEINTERFACE,
                "androidx/sqlite/db/SupportSQLiteStatement",
                "executeInsert",
                "()J",
                true,
            )
            visitInsn(Opcodes.POP2)
            visitLabel(tryEnd)
            visitInsn(Opcodes.ICONST_0)
            visitInsn(Opcodes.IRETURN)
            visitLabel(userHandler)
            visitVarInsn(Opcodes.ASTORE, 1)
            visitInsn(Opcodes.ICONST_1)
            visitInsn(Opcodes.IRETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun databaseTransactionFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/TransactionSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "transaction",
            "(Landroid/database/sqlite/SQLiteDatabase;Landroid/database/sqlite/SQLiteTransactionListener;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/database/sqlite/SQLiteDatabase",
                "beginTransactionWithListenerNonExclusive",
                "(Landroid/database/sqlite/SQLiteTransactionListener;)V",
                false,
            )
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/database/sqlite/SQLiteDatabase",
                "setTransactionSuccessful",
                "()V",
                false,
            )
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "android/database/sqlite/SQLiteDatabase",
                "endTransaction",
                "()V",
                false,
            )
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun kotlinStyleConstructorWithExceptionHandlerFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(Opcodes.ACC_PUBLIC, "<init>", "(Ljava/lang/Object;)V", null, null).apply {
            visitAnnotation(OPERATION_DESCRIPTOR, false).finishStringValue("constructor-with-handler")
            val tryStart = Label()
            val tryEnd = Label()
            val handler = Label()
            val returnLabel = Label()
            visitTryCatchBlock(tryStart, tryEnd, handler, "java/lang/RuntimeException")
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 1)
            visitLdcInsn("value")
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "kotlin/jvm/internal/Intrinsics",
                "checkNotNullParameter",
                "(Ljava/lang/Object;Ljava/lang/String;)V",
                false,
            )
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(Opcodes.INVOKESPECIAL, "java/lang/Object", "<init>", "()V", false)
            visitLabel(tryStart)
            visitLdcInsn("1")
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                "java/lang/Integer",
                "parseInt",
                "(Ljava/lang/String;)I",
                false,
            )
            visitInsn(Opcodes.POP)
            visitLabel(tryEnd)
            visitJumpInsn(Opcodes.GOTO, returnLabel)
            visitLabel(handler)
            visitInsn(Opcodes.POP)
            visitLabel(returnLabel)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun webSocketFixture(): ByteArray {
        val writer = SafeClassWriter(null, ClassWriter.COMPUTE_FRAMES or ClassWriter.COMPUTE_MAXS)
        writer.visit(Opcodes.V17, Opcodes.ACC_PUBLIC, "example/AsmSmoke", null, "java/lang/Object", null)
        writer.visitMethod(
            Opcodes.ACC_PUBLIC or Opcodes.ACC_STATIC,
            "openSocket",
            "(Lokhttp3/OkHttpClient;Lokhttp3/Request;Lokhttp3/WebSocketListener;)V",
            null,
            null,
        ).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitVarInsn(Opcodes.ALOAD, 1)
            visitVarInsn(Opcodes.ALOAD, 2)
            val line = Label()
            visitLabel(line)
            visitLineNumber(91, line)
            visitMethodInsn(
                Opcodes.INVOKEVIRTUAL,
                "okhttp3/OkHttpClient",
                "newWebSocket",
                "(Lokhttp3/Request;Lokhttp3/WebSocketListener;)Lokhttp3/WebSocket;",
                false,
            )
            visitInsn(Opcodes.POP)
            visitInsn(Opcodes.RETURN)
            visitMaxs(0, 0)
            visitEnd()
        }
        writer.visitEnd()
        return writer.toByteArray()
    }

    private fun collectCalls(bytes: ByteArray): Set<Call> {
        val calls = linkedSetOf<Call>()
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor {
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            calls += Call(owner, methodName)
                        }
                    }
                }
            },
            0,
        )
        return calls
    }

    private fun verifyWithoutLoadingAndroidTypes(bytes: ByteArray) {
        val node = ClassNode(Opcodes.ASM9)
        ClassReader(bytes).accept(node, 0)
        for (method in node.methods) {
            Analyzer(BasicVerifier()).analyze(node.name, method)
        }
    }

    private fun stringConstants(bytes: ByteArray, targetMethod: String): Set<String> {
        val constants = linkedSetOf<String>()
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor? {
                    if (name != targetMethod) return null
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitLdcInsn(value: Any?) {
                            if (value is String) constants += value
                        }
                    }
                }
            },
            0,
        )
        return constants
    }

    private data class Call(val owner: String, val name: String)

    private fun methodCalls(bytes: ByteArray, targetMethod: String): Set<Call> {
        val calls = linkedSetOf<Call>()
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor {
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            owner: String,
                            methodName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (name == targetMethod) {
                                calls += Call(owner, methodName)
                            }
                        }
                    }
                }
            },
            0,
        )
        return calls
    }

    private fun countMethodCalls(bytes: ByteArray, targetMethod: String, owner: String, methodName: String): Int {
        var count = 0
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitMethod(
                    access: Int,
                    name: String,
                    descriptor: String,
                    signature: String?,
                    exceptions: Array<out String>?,
                ): MethodVisitor? {
                    if (name != targetMethod) return null
                    return object : MethodVisitor(Opcodes.ASM9) {
                        override fun visitMethodInsn(
                            opcodeAndSource: Int,
                            callOwner: String,
                            callName: String,
                            descriptor: String,
                            isInterface: Boolean,
                        ) {
                            if (callOwner == owner && callName == methodName) count += 1
                        }
                    }
                }
            },
            0,
        )
        return count
    }

    private fun countClassAnnotation(bytes: ByteArray, descriptor: String): Int {
        var count = 0
        ClassReader(bytes).accept(
            object : ClassVisitor(Opcodes.ASM9) {
                override fun visitAnnotation(annotationDescriptor: String, visible: Boolean): AnnotationVisitor? {
                    if (annotationDescriptor == descriptor) count += 1
                    return null
                }
            },
            0,
        )
        return count
    }

    private class SafeClassWriter(
        reader: ClassReader?,
        flags: Int,
    ) : ClassWriter(reader, flags) {
        override fun getCommonSuperClass(type1: String, type2: String): String = "java/lang/Object"
    }

    private companion object {
        private const val OPERATION_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterOperation;"
        private const val MIXED_DESCRIPTOR =
            "(Landroid/os/Handler;Ljava/lang/Runnable;Ljava/util/concurrent/Executor;" +
                "Lkotlinx/coroutines/CoroutineScope;Lkotlin/coroutines/CoroutineContext;" +
                "Lkotlinx/coroutines/CoroutineStart;Lkotlin/jvm/functions/Function2;" +
                "Landroid/view/View;Landroid/view/View\$OnClickListener;" +
                "Lokhttp3/OkHttpClient\$Builder;Lokhttp3/OkHttpClient;Lokhttp3/Request;" +
                "Lokhttp3/WebSocketListener;Lokhttp3/EventListener\$Factory;Lokhttp3/EventListener;)V"
    }
}
