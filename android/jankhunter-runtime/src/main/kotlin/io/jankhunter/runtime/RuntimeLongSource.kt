package io.jankhunter.runtime

internal fun interface RuntimeBooleanSource {
    fun getAsBoolean(): Boolean
}

internal fun interface RuntimeIntSource {
    fun getAsInt(): Int
}

internal fun interface RuntimeLongSource {
    fun getAsLong(): Long
}

internal fun interface RuntimeLongConsumer {
    fun accept(value: Long)
}

internal fun interface RuntimeLongOperator {
    fun apply(value: Long): Long
}
