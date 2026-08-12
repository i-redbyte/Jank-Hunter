package io.jankhunter.artti.internal

internal enum class ArtTiNativeStatus(val wireValue: Int) {
    OK(0),
    INVALID_ARGUMENT(1),
    INVALID_STATE(2),
    QUEUE_FULL(3),
    CONTENDED(4),
    CAPACITY_EXHAUSTED(5),
    NOT_FOUND(6),
    CLOSED(7),
    BUFFER_TOO_SMALL(8),
    CORRUPT_INPUT(9),
    UNSUPPORTED(10),
    INTERNAL(11),
    UNKNOWN(Int.MIN_VALUE),
    ;

    companion object {
        fun fromWire(value: Int): ArtTiNativeStatus = entries.firstOrNull { it.wireValue == value } ?: UNKNOWN
    }
}
