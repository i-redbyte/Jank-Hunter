package com.example.jhsmoke

import android.app.Activity
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import io.jankhunter.annotations.JankHunterIgnore
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterTelemetry
import java.io.File

@JankHunterIgnore
object IdentityExecutionProbe {
    @JvmStatic
    fun run(activity: Activity) {
        capture(activity, "main")
        activity.startService(Intent(activity, IdentityService::class.java))
    }

    fun capture(context: Context, role: String) {
        JankHunter.init(context.applicationContext)
        check(JankHunter.reconfigure("identity-$role") {
            it.mainProcessOnly(false).logDirectory(File(context.filesDir, "identity-${android.os.Process.myPid()}"))
        })
        JankHunterTelemetry.counter("probe.identity.$role", 1)
        Handler(Looper.getMainLooper()).postDelayed({
            val archive = File(context.getExternalFilesDir(null), "identity-$role-${android.os.Process.myPid()}-${System.nanoTime()}.zip")
            check(JankHunter.captureLogArchiveAsync(archive) { result ->
                checkNotNull(result) { "Identity archive failed" }
                Log.i("JHIDENTITYR8", "EXECUTION PASS role=$role archive=$archive")
            })
        }, 800)
    }
}

@JankHunterIgnore
class IdentityService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        IdentityExecutionProbe.capture(this, "remote")
        return START_NOT_STICKY
    }
}
