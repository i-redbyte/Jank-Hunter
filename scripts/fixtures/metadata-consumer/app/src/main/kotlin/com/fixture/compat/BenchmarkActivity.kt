package com.fixture.compat

import android.app.Activity
import android.os.Bundle
import android.view.View
import android.view.MotionEvent
import android.graphics.Canvas
import android.graphics.Color
import android.os.SystemClock

class BenchmarkActivity : Activity() {
    var lastInputAt = 0L
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(object : View(this) {
            override fun onDraw(canvas: Canvas) {
                canvas.drawColor(if (SystemClock.uptimeMillis() % 1000 < 500) Color.BLUE else Color.RED)
                postInvalidateOnAnimation()
            }
        })
    }
    override fun dispatchTouchEvent(event: MotionEvent): Boolean {
        lastInputAt = SystemClock.elapsedRealtimeNanos()
        return true
    }
}
