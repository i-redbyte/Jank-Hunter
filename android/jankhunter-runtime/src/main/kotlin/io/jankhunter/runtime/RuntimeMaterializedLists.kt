package io.jankhunter.runtime

/** Exact JDK list types whose accessors cannot dispatch to application code. */
internal fun isMaterialized(requests: List<*>): Boolean {
    if (requests === emptyList<Any>()) return true
    return when (requests.javaClass.name) {
        "java.util.ArrayList", "java.util.Arrays\$ArrayList", "java.util.Collections\$SingletonList",
        "java.util.Collections\$EmptyList", "java.util.ImmutableCollections\$List12",
        "java.util.ImmutableCollections\$ListN" -> true
        else -> false
    }
}
