package io.jankhunter.okhttp3

internal fun interface NetworkLongSource {
    fun getAsLong(): Long
}

internal fun interface NetworkBooleanSource {
    fun getAsBoolean(): Boolean
}
