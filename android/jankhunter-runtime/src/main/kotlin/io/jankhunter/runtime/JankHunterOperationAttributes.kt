package io.jankhunter.runtime

/**
 * Immutable, bounded dimensions attached to an operation.
 *
 * Instances are reusable: cache common combinations instead of rebuilding them for every
 * operation. Values must be low-cardinality categories, never entity or user identifiers.
 */
class JankHunterOperationAttributes private constructor(
    private val entries: Array<String>,
) {
    val size: Int
        get() = entries.size ushr 1

    internal fun key(index: Int): String = entries[index shl 1]

    internal fun value(index: Int): String = entries[(index shl 1) + 1]

    companion object {
        const val MAX_SIZE = 8

        @JvmField
        val EMPTY = JankHunterOperationAttributes(emptyArray())

        @JvmStatic
        fun of(key: String, value: String): JankHunterOperationAttributes =
            create(arrayOf(key, value))

        @JvmStatic
        fun of(
            key1: String,
            value1: String,
            key2: String,
            value2: String,
        ): JankHunterOperationAttributes = create(arrayOf(key1, value1, key2, value2))

        @JvmStatic
        fun of(
            key1: String,
            value1: String,
            key2: String,
            value2: String,
            key3: String,
            value3: String,
        ): JankHunterOperationAttributes = create(arrayOf(key1, value1, key2, value2, key3, value3))

        @JvmStatic
        fun of(
            key1: String,
            value1: String,
            key2: String,
            value2: String,
            key3: String,
            value3: String,
            key4: String,
            value4: String,
        ): JankHunterOperationAttributes = create(
            arrayOf(key1, value1, key2, value2, key3, value3, key4, value4),
        )

        /** The array contains alternating key/value entries and is defensively copied. */
        @JvmStatic
        fun fromEntries(vararg entries: String): JankHunterOperationAttributes =
            create(Array(entries.size) { index -> entries[index] })

        private fun create(entries: Array<String>): JankHunterOperationAttributes {
            require(entries.size and 1 == 0) { "Operation attributes must contain key/value pairs" }
            require(entries.size <= MAX_SIZE * 2) { "Operation attributes exceed $MAX_SIZE pairs" }
            var index = 0
            while (index < entries.size) {
                val key = entries[index]
                val value = entries[index + 1]
                require(key.isNotBlank() && key != "unknown") { "Operation attribute key must be known" }
                require(value.isNotBlank() && value != "unknown") { "Operation attribute value must be known" }
                var previous = 0
                while (previous < index) {
                    require(entries[previous] != key) { "Operation attribute key '$key' is duplicated" }
                    previous += 2
                }
                index += 2
            }
            return if (entries.isEmpty()) EMPTY else JankHunterOperationAttributes(entries)
        }
    }
}
