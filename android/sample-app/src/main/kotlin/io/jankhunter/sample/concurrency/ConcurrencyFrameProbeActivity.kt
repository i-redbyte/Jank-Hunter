package io.jankhunter.sample.concurrency

import android.app.Activity
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.os.Bundle
import android.os.SystemClock
import android.view.View
import android.view.WindowManager

/** A constant drawing workload for comparing recording modes on the same device. */
class ConcurrencyFrameProbeActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        setContentView(ProbeView(this))
    }

    private class ProbeView(context: Context) : View(context) {
        private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = Color.rgb(65, 155, 210) }

        override fun onDraw(canvas: Canvas) {
            super.onDraw(canvas)
            canvas.drawColor(Color.rgb(12, 27, 22))
            val fraction = (SystemClock.uptimeMillis() % 2_000L).toFloat() / 2_000f
            canvas.drawCircle(width * fraction, height / 2f, 24f * resources.displayMetrics.density, paint)
            postInvalidateOnAnimation()
        }
    }
}
