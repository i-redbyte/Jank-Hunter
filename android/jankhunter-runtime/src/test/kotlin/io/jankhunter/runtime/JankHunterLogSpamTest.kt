package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import java.io.File
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterLogSpamTest {
    @Test
    fun maxKeysIsPreservedUnderConcurrentUniqueSources() {
        val directory = Files.createTempDirectory("jankhunter-log-spam-test").toFile()
        val pool = Executors.newFixedThreadPool(64)
        try {
            JankHunter.shutdown()
            JankHunter.init(
                TestContext(directory),
                JankHunterConfig.builder()
                    .autoStartCollectors(false)
                    .flushIntervalMs(60_000)
                    .logCompressionEnabled(false)
                    .maxLogSpamKeys(4)
                    .build(),
            )
            setLastLogSpamFlushAt(Long.MAX_VALUE)

            val ready = CountDownLatch(64)
            val start = CountDownLatch(1)
            repeat(64) { index ->
                pool.execute {
                    ready.countDown()
                    assertTrue(start.await(2, TimeUnit.SECONDS))
                    JankHunter.recordLogSpam("Owner-$index", "android.util.Log.d.$index", 3)
                }
            }

            assertTrue(ready.await(2, TimeUnit.SECONDS))
            start.countDown()
        } finally {
            pool.shutdown()
            assertTrue(pool.awaitTermination(2, TimeUnit.SECONDS))
        }

        try {
            assertTrue("log spam counter size exceeded cap: ${logSpamCounterSize()}", logSpamCounterSize() <= 4)
        } finally {
            JankHunter.shutdown()
            directory.deleteRecursively()
        }
    }

    @Suppress("UNCHECKED_CAST")
    private fun logSpamCounterSize(): Int {
        val field = JankHunter::class.java.getDeclaredField("logSpamCounters").apply {
            isAccessible = true
        }
        return (field.get(JankHunter) as Map<Any, Any>).size
    }

    private fun setLastLogSpamFlushAt(value: Long) {
        val field = JankHunter::class.java.getDeclaredField("lastLogSpamFlushAtMs").apply {
            isAccessible = true
        }
        (field.get(JankHunter) as AtomicLong).set(value)
    }

    private class TestContext(
        private val rootDir: File,
    ) : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this

        override fun getPackageName(): String = "com.example"

        override fun getFilesDir(): File = rootDir

        override fun getSystemService(name: String): Any? = null
    }
}
