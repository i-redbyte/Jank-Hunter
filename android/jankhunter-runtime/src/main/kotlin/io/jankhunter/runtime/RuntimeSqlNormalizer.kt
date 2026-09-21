package io.jankhunter.runtime

import io.jankhunter.sql.SqlNormalizer

/** Runtime fallback for SQL supplied through generated Room lambda arguments. */
internal object RuntimeSqlNormalizer {
    fun normalize(value: String?): String? = SqlNormalizer.normalize(value)

    fun operation(normalizedQuery: String?, fallback: Int): Int =
        SqlNormalizer.operation(normalizedQuery, fallback)

    fun fingerprint(normalizedQuery: String?): Long = SqlNormalizer.fingerprint(normalizedQuery)

}
