package com.example.jhsmoke

import android.app.Activity
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.lifecycle.MutableLiveData
import androidx.lifecycle.Observer
import io.jankhunter.annotations.JankHunterIgnore
import io.jankhunter.runtime.JankHunter
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/** A real main-thread monitor wait inside Kotlin Lazy, with the application caller below it. */
@JankHunterIgnore
object StallExecutionProbe {
    @JvmStatic
    fun run(activity: Activity) {
        check(JankHunter.reconfigure("stall-r8-probe") {
            it.mainThreadStallThresholdMs(100)
                .logDirectory(File(activity.filesDir, "stall-probe-${android.os.Process.myPid()}"))
        })
        val main = Handler(Looper.getMainLooper())
        val entered = CountDownLatch(1)
        val lock = Any()
        val value = lazy(lock) { "initialized" }
        val signal = MutableLiveData<Int>()
        signal.observeForever(object : Observer<Int> {
            @JankHunterIgnore
            override fun onChanged(valueIgnored: Int) {
                entered.countDown()
                Log.i("JHSTALLR8", "RESULT ${value.value}")
            }
        })
        Thread({
            synchronized(lock) {
                main.post { signal.value = 1 }
                check(entered.await(3, TimeUnit.SECONDS))
                Thread.sleep(1200)
            }
        }, "stall-probe-lock").start()
        main.postDelayed({
            val archive = File(activity.getExternalFilesDir(null), "stall-execution-${android.os.Process.myPid()}-${System.nanoTime()}.zip")
            check(JankHunter.captureLogArchiveAsync(archive) { result ->
                if (result == null) Log.e("JHSTALLR8", "EXECUTION FAIL archive")
                else Log.i("JHSTALLR8", "EXECUTION PASS archive=$archive")
            })
        }, 3000)
    }
}
