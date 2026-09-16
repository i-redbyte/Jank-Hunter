package io.jankhunter.runtime

/** Receives the result of a background snapshot or archive capture. */
fun interface JankHunterCaptureCallback<T> {
    fun onComplete(result: T?)
}
