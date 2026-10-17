package io.jankhunter.runtime

import android.content.Context
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.After
import org.junit.Before
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

/** Same lifecycle and hook assertions run on host JVM and Android/ART. */
abstract class RuntimeEnabledStateContract {
    protected abstract fun testContext(): Context
    protected abstract fun testDirectory(): File

    @Before
    fun isolateRuntimeFromProviderStartup() = JankHunter.shutdown()

    @After
    fun stopRuntime() = JankHunter.shutdown()

    @Test
    fun busyStorageSelectionFailsClosedAndCanBeExplicitlyRetried() {
        val noBackup = File(testDirectory().parentFile, "selection-state").apply { mkdirs() }
        val stateDirectory = File(noBackup, "jankhunter-storage").apply { mkdirs() }
        val base = testContext()
        val context = object : android.content.ContextWrapper(base) {
            override fun getApplicationContext(): Context = this
            override fun getPackageName(): String = base.packageName
            override fun getFilesDir(): File = base.filesDir
            override fun getNoBackupFilesDir(): File = noBackup
        }
        val policy = JankHunterStoragePolicy(File(testDirectory().parentFile, "selected"),
            1_000_000L, 10_000_000L, setOf("jhlog", "hprof"), setOf("hprof"), 1024, true)
        java.io.RandomAccessFile(File(stateDirectory, ".storage-directory.lock"), "rw").use { access ->
            access.channel.lock().use {
                JankHunter.init(context, JankHunterConfig.builder().autoStartCollectors(false)
                    .storagePolicy(policy).logDirectory(testDirectory()).build())
                assertFalse(JankHunter.isStarted())
                assertTrue("A storage fault must preserve the user's requested runtime state", JankHunter.isRuntimeEnabled())
                assertEquals(0L, JankHunterHooks.enterMethod(9010, "busy-selection"))
                assertEquals("IOException", JankHunter.initDiagnostics().failureClass)
                assertTrue(JankHunter.setRuntimeEnabled(false))
            }
        }
        assertTrue(JankHunter.setRuntimeEnabled(true))
        assertTrue(JankHunter.isStarted())
        val token = JankHunterHooks.enterMethod(9010, "retried-selection")
        assertTrue("Explicit retry must restore hook admission", token != 0L)
        JankHunterHooks.exitMethod(token, 9010)
    }

    @Test
    fun injectedObservationNeverCallsHostThreadOrListAccessors() {
        JankHunter.init(testContext(), JankHunterConfig.builder().autoStartCollectors(false)
            .workerTracingEnabled(true).threadSamplerEnabled(true).logDirectory(testDirectory()).build())
        assertTrue(JankHunter.isStarted())
        assertTrue(JankHunterHooks.workSubmissionEpoch() != 0L)
        val calls = AtomicInteger()
        val thread = object : Thread() {
            override fun getId(): Long { calls.incrementAndGet(); error("host getId") }
            override fun getState(): State { calls.incrementAndGet(); error("host getState") }
        }
        JankHunterHooks.observeThreadConstructed(thread, 1, "host", 1, 1)
        JankHunterHooks.observeThreadFactory(thread, null, "host")
        val start = JankHunterHooks.beginThreadStart(thread, 1, "host", 1, 1)
        JankHunterHooks.threadStartReturned(thread, start)
        JankHunterHooks.observeThreadEnd(thread)
        val parents = object : AbstractList<Any>() {
            override val size: Int get() { calls.incrementAndGet(); error("host list size") }
            override fun get(index: Int): Any { calls.incrementAndGet(); error("host list get") }
        }
        JankHunterHooks.observeWorkChainCombineRequests(parents, Any())
        assertEquals("Telemetry dispatched an application accessor", 0, calls.get())
    }

    @Test
    fun replacementInitFromStartupCallbackKeepsItsOwnConfigurationAndHooks() {
        val context = testContext()
        JankHunter.init(context, JankHunterConfig.builder().autoStartCollectors(false)
            .logDirectory(testDirectory()).processNameRedactor(JankHunterProcessNameRedactor { name ->
                JankHunter.shutdown()
                JankHunter.init(context, JankHunterConfig.builder().autoStartCollectors(false)
                    .logDirectory(testDirectory()).httpSlowThresholdMs(7890L)
                    .runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, false).build())
                name
            }).build())
        assertTrue(JankHunter.isStarted())
        val task = Runnable { }
        assertSame(task, JankHunterHooks.wrapRunnable(task, "replacement-init"))
        assertTrue(JankHunter.reconfigure { builder -> assertEquals(7890L, builder.build().httpSlowThresholdMs()) })
        assertSame(task, JankHunterHooks.wrapRunnable(task, "replacement-init"))
    }

    @Test
    fun successfulShutdownCallbackCannotResurrectTheAbandonedStartup() {
        JankHunter.init(testContext(), JankHunterConfig.builder().autoStartCollectors(false)
            .logDirectory(testDirectory()).processNameRedactor(JankHunterProcessNameRedactor { name ->
                JankHunter.shutdown()
                name
            }).build())
        assertFalse("An explicitly abandoned startup became active again", JankHunter.isStarted())
        assertEquals(0L, JankHunterHooks.enterMethod(9001L, "abandoned-startup"))
        assertFalse(JankHunter.setRuntimeEnabled(true))
    }

    @Test
    fun successfulDisableCallbackStopsBeforeAllocatingAWriter() {
        val opens = AtomicInteger()
        val storage = object : JankHunterBinaryStorage {
            override val fileSizeLimitBytes = Long.MAX_VALUE
            override val archivesSizeLimitBytes = Long.MAX_VALUE
            override fun openWriter(fileName: String): JankHunterBinaryWriter {
                opens.incrementAndGet()
                error("Disabled startup must not open a writer")
            }
            override fun createArtifact(fileName: String): JankHunterBinaryArtifact = error("Must not create")
            override fun cleanup(protectedPaths: Set<String>) = Unit
            override fun listFiles(): List<String> = emptyList()
        }
        JankHunter.init(testContext(), JankHunterConfig.builder().autoStartCollectors(false)
            .binaryStorage(storage).logDirectory(testDirectory()).processNameRedactor(JankHunterProcessNameRedactor { name ->
                check(JankHunter.setRuntimeEnabled(false))
                name
            }).build())
        assertDisabled()
        assertEquals("Disabled startup opened a log writer", 0, opens.get())
    }

    @Test
    fun disableDuringRosterRedactionStopsBeforeOpeningAWriter() {
        val calls = AtomicInteger()
        val opens = AtomicInteger()
        val storage = object : JankHunterBinaryStorage {
            override val fileSizeLimitBytes = Long.MAX_VALUE
            override val archivesSizeLimitBytes = Long.MAX_VALUE
            override fun openWriter(fileName: String): JankHunterBinaryWriter {
                opens.incrementAndGet()
                error("Disabled roster must not open a writer")
            }
            override fun createArtifact(fileName: String): JankHunterBinaryArtifact = error("Must not create")
            override fun cleanup(protectedPaths: Set<String>) = Unit
            override fun listFiles(): List<String> = emptyList()
        }
        JankHunter.init(testContext(), JankHunterConfig.builder().autoStartCollectors(false)
            .binaryStorage(storage).logDirectory(testDirectory())
            .processNameRedactor(JankHunterProcessNameRedactor { name ->
                if (calls.incrementAndGet() == 2) check(JankHunter.setRuntimeEnabled(false))
                name
            }).build())
        assertTrue("Roster callback was not exercised", calls.get() >= 2)
        assertDisabled()
        assertEquals("Disabled roster opened a log writer", 0, opens.get())
    }

    @Test
    fun injectedPriorityHandoffKeepsNativeComparatorTaskIdentity() {
        initialize()
        val queue = java.util.concurrent.PriorityBlockingQueue<Runnable>(11,
            Comparator { left, right -> (left as PriorityCommand).priority.compareTo((right as PriorityCommand).priority) })
        val pool = java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS, queue)
        try {
            val original = PriorityCommand(1)
            assertSame(original, JankHunterHooks.wrapRunnable(pool, original, "legacy-priority"))
            val prepared = JankHunterHooks.prepareExecutorRunnable(pool, original, 1L, "priority", "priority",
                1, 1, 0L, 0L, null, 0)
            try {
                queue.add(checkNotNull(prepared))
                queue.add(PriorityCommand(2))
                assertSame(original, queue.poll())
            } finally { JankHunterHooks.executorHandoffReturned(prepared, null) }
        } finally { pool.shutdownNow() }
    }

    @Test
    fun injectedCustomTaskFactoryReceivesOriginalRunnableAndCallable() {
        initialize()
        val pool = object : java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS,
            java.util.concurrent.LinkedBlockingQueue()) {
            override fun <T> newTaskFor(callable: java.util.concurrent.Callable<T>): java.util.concurrent.RunnableFuture<T> =
                java.util.concurrent.FutureTask(callable)
        }
        try {
            val command = Runnable { }
            assertSame(command, JankHunterHooks.wrapRunnable(pool, command, "legacy-factory"))
            val prepared = JankHunterHooks.prepareExecutorRunnable(pool, command, 2L, "factory", "factory",
                1, 1, 0L, 0L, null, 0)
            try { assertSame(command, prepared) }
            finally { JankHunterHooks.executorHandoffReturned(prepared, null) }
            val callable = java.util.concurrent.Callable { 42 }
            assertSame(callable, JankHunterHooks.wrapCallable(pool, callable, "legacy-factory"))
            val preparedCallable = JankHunterHooks.prepareExecutorCallable(pool, callable, 3L, "factory", "factory",
                1, 1, 0L, 0L, null, 0)
            try { assertSame(callable, preparedCallable) }
            finally { JankHunterHooks.executorHandoffReturned(preparedCallable, null) }
        } finally { pool.shutdownNow() }
    }

    private class PriorityCommand(val priority: Int) : Runnable {
        override fun run() = Unit
    }

    @Test
    fun injectedPoolCallbacksKeepOriginalTaskType() {
        initialize()
        val observed = java.util.concurrent.atomic.AtomicReference<Runnable>()
        val completed = CountDownLatch(1)
        val pool = object : java.util.concurrent.ThreadPoolExecutor(1, 1, 0L, TimeUnit.MILLISECONDS,
            java.util.concurrent.LinkedBlockingQueue()) {
            override fun beforeExecute(thread: Thread, command: Runnable) {
                observed.set(command)
            }
        }
        try {
            val command = Runnable { completed.countDown() }
            val prepared = JankHunterHooks.prepareExecutorRunnable(pool, command, 4L, "beforeExecute", "beforeExecute",
                1, 1, 0L, 0L, null, 0)
            try { pool.execute(checkNotNull(prepared)) }
            finally { JankHunterHooks.executorHandoffReturned(prepared, null) }
            assertTrue(completed.await(5, TimeUnit.SECONDS))
            assertSame("Native beforeExecute received a different task class", command, observed.get())
            val opaque = java.util.concurrent.Executor { task ->
                assertSame("Opaque executor received a replacement task", command, task)
                task.run()
            }
            val opaquePrepared = JankHunterHooks.prepareExecutorRunnable(opaque, command, 5L, "opaque", "opaque",
                1, 1, 0L, 0L, null, 0)
            try { opaque.execute(checkNotNull(opaquePrepared)) }
            finally { JankHunterHooks.executorHandoffReturned(opaquePrepared, null) }
        } finally { pool.shutdownNow() }
    }

    @Test
    fun writerStorageCallbackCannotEnterPublicSynchronousCapture() {
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val complete = CountDownLatch(1)
        val worker = java.util.concurrent.atomic.AtomicReference<Thread>()
        val results = java.util.Collections.synchronizedList(mutableListOf<Boolean>())
        val destination = File(testDirectory().parentFile, "self-capture")
        val storage = object : JankHunterBinaryStorage {
            override val fileSizeLimitBytes = Long.MAX_VALUE
            override val archivesSizeLimitBytes = Long.MAX_VALUE
            override fun openWriter(fileName: String): JankHunterBinaryWriter {
                worker.set(Thread.currentThread())
                entered.countDown()
                check(release.await(5, TimeUnit.SECONDS))
                results += JankHunter.captureLogSnapshot() == null
                results += JankHunter.captureLogArchive(File(destination, "log.zip")) == null
                results += JankHunter.captureSessionArchives(destination) == null
                results += JankHunter.captureSessionArchives(destination, false) == null
                results += JankHunter.captureSessionArchives(destination, 0L) == null
                results += JankHunter.captureSessionArchiveArtifacts(destination) == null
                complete.countDown()
                error("End callback-only fixture")
            }
            override fun createArtifact(fileName: String): JankHunterBinaryArtifact = error("Must not create")
            override fun cleanup(protectedPaths: Set<String>) = Unit
            override fun listFiles(): List<String> = emptyList()
        }
        try {
            JankHunter.init(testContext(), JankHunterConfig.builder().autoStartCollectors(false)
                .binaryStorage(storage).logDirectory(testDirectory()).build())
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            release.countDown()
            assertTrue("Public capture waited on its own writer", complete.await(1, TimeUnit.SECONDS))
            assertEquals(List(6) { true }, results)
        } finally {
            release.countDown()
            if (complete.count != 0L) worker.get()?.interrupt()
        }
    }

    @Test
    fun startupCallbackCannotReplaceConfigurationDuringItsOwnStart() {
        val first = AtomicBoolean(true)
        val nestedResult = java.util.concurrent.atomic.AtomicReference<Boolean>()
        JankHunter.init(testContext(), JankHunterConfig.builder().autoStartCollectors(false)
            .logDirectory(testDirectory())
            .processNameRedactor(JankHunterProcessNameRedactor { name ->
                if (first.getAndSet(false)) nestedResult.set(JankHunter.reconfigure { it.enabled(false) })
                name
            }).build())
        assertEquals(false, nestedResult.get())
        assertHooksActive()
    }

    @Test
    fun storagePolicyRejectsExternalStorageWithoutCallingItOrStoppingHooks() {
        initialize()
        changeStorage()
        val calls = AtomicInteger()
        val external = object : JankHunterBinaryStorage {
            override val fileSizeLimitBytes = Long.MAX_VALUE
            override val archivesSizeLimitBytes = Long.MAX_VALUE
            override fun openWriter(fileName: String): JankHunterBinaryWriter {
                calls.incrementAndGet()
                error("An incompatible storage must not be opened")
            }
            override fun createArtifact(fileName: String): JankHunterBinaryArtifact {
                calls.incrementAndGet()
                error("An incompatible storage must not create artifacts")
            }
            override fun cleanup(protectedPaths: Set<String>) { calls.incrementAndGet() }
            override fun listFiles(): List<String> { calls.incrementAndGet(); return emptyList() }
        }
        assertEquals(JankHunterStorageSwitchResult.FAILED, JankHunter.switchBinaryStorage(external))
        assertEquals(0, calls.get())
        assertHooksActive()
    }

    @Test
    fun disabledRuntimeSurvivesStorageChangesInBothOrdersWithoutStarting() {
        for (viaUpdater in listOf(false, true)) {
            for (storageFirst in listOf(false, true)) {
                val starts = AtomicInteger()
                initialize(starts = starts)
                if (storageFirst) changeStorage()
                disable(viaUpdater)
                val before = starts.get()
                assertDisabled()
                changeStorage()
                assertDisabled()
                assertEquals("Storage update briefly started a disabled runtime", before, starts.get())
                JankHunter.shutdown()
            }
        }
    }

    @Test
    fun bothExplicitEnableApisRestoreActualHooksAfterEitherDisableApi() {
        for (disableUpdater in listOf(false, true)) {
            for (enableUpdater in listOf(false, true)) {
                initialize()
                disable(disableUpdater)
                changeStorage()
                assertDisabled()
                if (enableUpdater) {
                    assertTrue(JankHunter.reconfigure { it.runtimeEnabled(true) })
                } else {
                    assertTrue(JankHunter.setRuntimeEnabled(true))
                }
                assertHooksActive()
                JankHunter.shutdown()
            }
        }
    }

    @Test
    fun directEnableOverridesInitiallyDisabledConfiguration() {
        initialize(enabled = false)
        assertDisabled()
        assertTrue(JankHunter.setRuntimeEnabled(true))
        assertHooksActive()
        changeStorage()
        assertHooksActive()
    }

    @Test
    fun deferredObservationChoiceSurvivesStorageUpdatesWithoutChangingActiveMode() {
        initialize()
        val active = JankHunter.executionObservationModes()
        val requested = JankHunterExecutionObservationModes(
            JankHunterExecutionObservationMode.IN_PLACE, JankHunterExecutionObservationMode.IN_PLACE)
        assertTrue(JankHunter.reconfigure { builder ->
            builder.taskObservationMode(requested.tasks).coroutineObservationMode(requested.coroutines)
        })
        assertEquals(active, JankHunter.executionObservationModes())
        assertEquals(requested, JankHunter.requestedExecutionObservationModes())
        changeStorage()
        assertEquals(active, JankHunter.executionObservationModes())
        assertEquals(requested, JankHunter.requestedExecutionObservationModes())
    }

    @Test
    fun failedEnableReconfigurationRestoresDisabledRequest() {
        initialize()
        disable(viaUpdater = false)
        assertFalse(JankHunter.reconfigure { builder ->
            builder.runtimeEnabled(true)
                .processNameRedactor(JankHunterProcessNameRedactor { error("replacement start failed") })
        })
        assertDisabled()
        changeStorage()
        assertDisabled()
        assertTrue(JankHunter.setRuntimeEnabled(true))
        assertHooksActive()
    }

    @Test
    fun failedUpdaterCannotPublishItsExplicitEnable() {
        initialize()
        disable(viaUpdater = true)
        assertFalse(JankHunter.reconfigure { builder ->
            builder.runtimeEnabled(true)
            error("updater failed")
        })
        changeStorage()
        assertDisabled()
    }

    @Test
    fun failedReplacementRestoresActiveHooksAndRequest() {
        initialize()
        assertFalse(JankHunter.reconfigure { builder ->
            builder.processNameRedactor(JankHunterProcessNameRedactor { error("replacement failed") })
        })
        assertHooksActive()
        changeStorage()
        assertHooksActive()
    }

    @Test
    fun startupFailureDoesNotBecomeAUserDisable() {
        val fail = AtomicBoolean(true)
        JankHunter.init(testContext(), JankHunterConfig.builder()
            .autoStartCollectors(false)
            .logDirectory(testDirectory())
            .processNameRedactor(JankHunterProcessNameRedactor { name ->
                check(!fail.getAndSet(false)) { "initial start failed" }
                name
            }).build())
        assertFalse(JankHunter.isStarted())
        assertTrue(JankHunter.isRuntimeEnabled())
        changeStorage()
        assertHooksActive()
    }

    @Test
    fun staleDisableCannotOverrideNewerExplicitEnableNoop() {
        initialize()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val executor = Executors.newSingleThreadExecutor()
        try {
            val update = executor.submit<Boolean> {
                JankHunter.reconfigure { builder ->
                    builder.runtimeEnabled(false)
                    entered.countDown()
                    check(release.await(10, TimeUnit.SECONDS))
                }
            }
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            assertTrue(JankHunter.setRuntimeEnabled(true))
            release.countDown()
            assertFalse(update.get(10, TimeUnit.SECONDS))
            assertHooksActive()
        } finally {
            release.countDown()
            executor.shutdownNow()
        }
    }

    @Test
    fun staleExplicitEnableCannotOverrideNewerDisable() {
        initialize()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val executor = Executors.newSingleThreadExecutor()
        try {
            val update = executor.submit<Boolean> {
                JankHunter.reconfigure { builder ->
                    builder.runtimeEnabled(true)
                    entered.countDown()
                    check(release.await(10, TimeUnit.SECONDS))
                }
            }
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            assertTrue(JankHunter.setRuntimeEnabled(false))
            release.countDown()
            assertFalse(update.get(10, TimeUnit.SECONDS))
            changeStorage()
            assertDisabled()
        } finally {
            release.countDown()
            executor.shutdownNow()
        }
    }

    @Test
    fun unrelatedUpdatesPreserveDisabledFeaturesAndExplicitEnableRestoresHooks() {
        initialize()
        assertTrue(JankHunter.reconfigure { builder ->
            builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, false)
                .runtimeCallGraphEnabled(false).httpSlowThresholdMs(4321L)
        })
        changeStorage()
        assertTrue(JankHunter.isStarted())
        val task = Runnable { }
        assertSame(task, JankHunterHooks.wrapRunnable(task, "disabled-feature"))
        assertEquals(0L, JankHunterHooks.enterMethod(9001L, "disabled-graph"))
        assertTrue(JankHunter.reconfigure { builder ->
            val config = builder.build()
            assertEquals(4321L, config.httpSlowThresholdMs())
            assertTrue(config.storagePolicy() != null)
            builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, true)
                .runtimeCallGraphEnabled(true)
        })
        assertHooksActive()
        assertTrue(JankHunter.setRuntimeEnabled(false))
        changeStorage()
        assertDisabled()
        assertTrue(JankHunter.setRuntimeEnabled(true))
        assertHooksActive()
    }

    @Test
    fun globalDisableSurvivesUpdatesAndDoesNotEraseDesiredRuntimeRequest() {
        val starts = AtomicInteger()
        initialize(starts = starts)
        assertTrue(JankHunter.reconfigure { it.enabled(false) })
        val before = starts.get()
        changeStorage()
        assertDisabled()
        assertEquals(before, starts.get())
        assertTrue(JankHunter.reconfigure { builder ->
            assertFalse(builder.build().enabled())
            assertTrue(builder.build().runtimeEnabled())
            builder.enabled(true)
        })
        assertHooksActive()
        assertTrue(JankHunter.setRuntimeEnabled(false))
        assertTrue(JankHunter.reconfigure { it.enabled(false) })
        assertTrue(JankHunter.reconfigure { it.enabled(true) })
        assertDisabled()
        assertTrue(JankHunter.setRuntimeEnabled(true))
        assertHooksActive()
    }

    @Test
    fun explicitEnableWhileGloballyDisabledIsRetainedUntilGlobalGateOpens() {
        initialize()
        assertTrue(JankHunter.setRuntimeEnabled(false))
        assertTrue(JankHunter.reconfigure { it.enabled(false) })
        assertFalse(JankHunter.setRuntimeEnabled(true))
        assertDisabled()
        changeStorage()
        assertDisabled()
        assertTrue(JankHunter.reconfigure { it.enabled(true) })
        assertHooksActive()
    }

    @Test
    fun initiallyDisallowedProcessCanBeReconfiguredWithoutLosingRuntimeIntent() {
        JankHunter.init(testContext(), JankHunterConfig.builder()
            .autoStartCollectors(false).logDirectory(testDirectory())
            .allowedProcesses(listOf("never.allowed.process")).build())
        assertFalse(JankHunter.isStarted())
        assertTrue(JankHunter.setRuntimeEnabled(false))
        changeStorage()
        assertDisabled()
        assertTrue(JankHunter.reconfigure { it.allowedProcesses(emptyList()) })
        assertDisabled()
        assertTrue(JankHunter.setRuntimeEnabled(true))
        assertHooksActive()
    }

    @Test
    fun explicitEnableWhileProcessBlockedWaitsForExplicitProcessPermission() {
        initialize()
        assertTrue(JankHunter.setRuntimeEnabled(false))
        assertTrue(JankHunter.reconfigure { it.allowedProcesses(listOf("never.allowed.process")) })
        assertFalse(JankHunter.setRuntimeEnabled(true))
        assertDisabled()
        changeStorage()
        assertDisabled()
        assertTrue(JankHunter.reconfigure { it.allowedProcesses(emptyList()) })
        assertHooksActive()
    }

    @Test
    fun allFeatureRequestsAndExplicitEmptyMasksSurviveUnrelatedUpdates() {
        initialize()
        for (enabled in listOf(false, true)) {
            assertTrue(JankHunter.reconfigure { builder ->
                JankHunterRuntimeFeature.entries.forEach { builder.runtimeFeatureEnabled(it, enabled) }
            })
            changeStorage()
            assertTrue(JankHunter.reconfigure { builder ->
                JankHunterRuntimeFeature.entries.forEach {
                    assertEquals("Lost request for $it", enabled, builder.build().isRuntimeFeatureEnabled(it))
                }
            })
        }
        assertHooksActive()
    }

    @Test
    fun failedUpdatesPreserveDisabledFeaturesAndStoragePolicyUntilExplicitEnable() {
        initialize()
        changeStorage()
        assertTrue(JankHunter.reconfigure { builder ->
            builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, false)
                .runtimeCallGraphEnabled(false).httpSlowThresholdMs(3456L)
        })
        for (startupFailure in listOf(false, true)) {
            assertFalse(JankHunter.reconfigure { builder ->
                builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, true)
                    .runtimeCallGraphEnabled(true).httpSlowThresholdMs(9999L)
                if (startupFailure) {
                    builder.processNameRedactor(JankHunterProcessNameRedactor { error("startup failed") })
                } else error("updater failed")
            })
            assertTrue(JankHunter.reconfigure { builder ->
                assertEquals(3456L, builder.build().httpSlowThresholdMs())
                assertTrue(builder.build().storagePolicy() != null)
            })
            assertTrue(JankHunter.isStarted())
            val task = Runnable { }
            assertSame(task, JankHunterHooks.wrapRunnable(task, "rollback-disabled-feature"))
            assertEquals(0L, JankHunterHooks.enterMethod(9001L, "rollback-disabled-graph"))
        }
        assertTrue(JankHunter.reconfigure { builder ->
            builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, true).runtimeCallGraphEnabled(true)
        })
        assertHooksActive()
    }

    @Test
    fun everyScalarSettingAndReferencedConfigurationValueSurvivesNoopUpdate() {
        initialize()
        var expected: JankHunterConfig? = null
        assertTrue(JankHunter.reconfigure { builder ->
            // Exercise every public scalar setter, including future flags added to the builder.
            JankHunterConfig.Builder::class.java.declaredMethods.filter {
                it.returnType == JankHunterConfig.Builder::class.java &&
                    it.parameterTypes.size == 1 && '$' !in it.name
            }.forEach { setter ->
                val getter = JankHunterConfig.Builder::class.java.getDeclaredField(setter.name)
                getter.isAccessible = true
                when (setter.parameterTypes.single()) {
                    java.lang.Boolean.TYPE -> setter.invoke(builder, !getter.getBoolean(builder))
                    java.lang.Long.TYPE -> setter.invoke(builder, getter.getLong(builder) + 7L)
                    java.lang.Integer.TYPE -> setter.invoke(builder, getter.getInt(builder) + 3)
                }
            }
            builder.logDirectory(testDirectory()).allowedProcesses(listOf(testContext().packageName))
                .processNameRedactor(JankHunterProcessNameRedactor { it })
            expected = builder.build()
        })
        assertTrue(JankHunter.reconfigure { builder ->
            val actual = builder.build()
            JankHunterConfig::class.java.declaredFields.filter {
                !java.lang.reflect.Modifier.isStatic(it.modifiers)
            }.forEach { field ->
                field.isAccessible = true
                val wanted = field.get(checkNotNull(expected))
                val found = field.get(actual)
                if (wanted is ByteArray) {
                    assertTrue("Lost ${field.name}", wanted.contentEquals(found as ByteArray))
                } else {
                    assertEquals("Lost ${field.name}", wanted, found)
                }
            }
        })
        assertDisabled()
    }

    @Test
    fun concurrentUnrelatedUpdatesComposeWithoutLosingDisabledFeature() {
        initialize()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val executor = Executors.newFixedThreadPool(2)
        try {
            val first = executor.submit<Boolean> {
                JankHunter.reconfigure { builder ->
                    builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, false)
                    entered.countDown()
                    check(release.await(10, TimeUnit.SECONDS))
                }
            }
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            val secondEntered = CountDownLatch(1)
            val second = executor.submit<Boolean> {
                secondEntered.countDown()
                JankHunter.reconfigure { it.httpSlowThresholdMs(5432L) }
            }
            assertTrue(secondEntered.await(5, TimeUnit.SECONDS))
            release.countDown()
            assertTrue(first.get(10, TimeUnit.SECONDS))
            assertTrue(second.get(10, TimeUnit.SECONDS))
            changeStorage()
            val task = Runnable { }
            assertSame(task, JankHunterHooks.wrapRunnable(task, "concurrent-feature"))
            assertTrue(JankHunter.reconfigure { builder ->
                assertEquals(5432L, builder.build().httpSlowThresholdMs())
            })
        } finally {
            release.countDown()
            executor.shutdownNow()
        }
    }

    @Test
    fun failingReplacementCannotUndoAnExplicitDisableFromStartupCallback() {
        initialize()
        assertFalse(JankHunter.reconfigure { builder ->
            builder.processNameRedactor(JankHunterProcessNameRedactor {
                check(JankHunter.setRuntimeEnabled(false))
                error("replacement failed after explicit disable")
            })
        })
        assertDisabled()
        changeStorage()
        assertDisabled()
        assertTrue(JankHunter.setRuntimeEnabled(true))
        assertHooksActive()
    }

    @Test
    fun failingReplacementCannotRestoreInitializationAfterShutdownCallback() {
        initialize()
        assertFalse(JankHunter.reconfigure { builder ->
            builder.processNameRedactor(JankHunterProcessNameRedactor {
                JankHunter.shutdown()
                error("replacement failed after shutdown")
            })
        })
        assertFalse(JankHunter.isStarted())
        assertFalse(JankHunter.setRuntimeEnabled(true))
    }

    @Test
    fun failingStaleUpdaterCannotOverwriteDiagnosticsOfNewerEnableRequest() {
        initialize()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val executor = Executors.newSingleThreadExecutor()
        try {
            val update = executor.submit<Boolean> {
                JankHunter.reconfigure {
                    entered.countDown()
                    check(release.await(10, TimeUnit.SECONDS))
                    error("stale updater failed")
                }
            }
            assertTrue(entered.await(5, TimeUnit.SECONDS))
            assertTrue(JankHunter.setRuntimeEnabled(true))
            val status = JankHunter.initDiagnostics().status
            release.countDown()
            assertFalse(update.get(10, TimeUnit.SECONDS))
            assertEquals(status, JankHunter.initDiagnostics().status)
            assertHooksActive()
        } finally {
            release.countDown()
            executor.shutdownNow()
        }
    }

    private fun initialize(enabled: Boolean = true, starts: AtomicInteger = AtomicInteger()) {
        JankHunter.init(testContext(), JankHunterConfig.builder()
            .autoStartCollectors(false)
            .runtimeEnabled(enabled)
            .logDirectory(testDirectory())
            .processNameRedactor(JankHunterProcessNameRedactor { name -> starts.incrementAndGet(); name })
            .build())
        if (enabled) assertHooksActive()
    }

    private fun disable(viaUpdater: Boolean) {
        if (viaUpdater) assertTrue(JankHunter.reconfigure { it.runtimeEnabled(false) })
        else assertTrue(JankHunter.setRuntimeEnabled(false))
    }

    private fun changeStorage() {
        val policy = JankHunterStoragePolicy(testDirectory(), Long.MAX_VALUE, Long.MAX_VALUE,
            setOf("jhlog"), emptySet(), 8192, true)
        assertTrue(JankHunter.reconfigure("storage_update") { it.storagePolicy(policy) })
    }

    private fun assertDisabled() {
        assertFalse(JankHunter.isStarted())
        assertFalse(JankHunter.isRuntimeEnabled())
        val task = Runnable { }
        assertSame(task, JankHunterHooks.wrapRunnable(task, "runtime-state"))
        assertEquals(0L, JankHunterHooks.enterMethod(9001L, "disabled-runtime"))
    }

    private fun assertHooksActive() {
        assertTrue(JankHunter.isStarted())
        assertTrue(JankHunter.isRuntimeEnabled())
        val task = Runnable { }
        assertNotSame(task, JankHunterHooks.wrapRunnable(task, "runtime-state"))
        val token = JankHunterHooks.enterMethod(9001L, "active-runtime")
        assertTrue("CALL_GRAPH hook must admit a new event", token != 0L)
        JankHunterHooks.exitMethod(token, 9001L)
    }
}
