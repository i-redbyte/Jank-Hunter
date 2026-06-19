package io.jankhunter.sample

internal object LeakCanaryBridge {
    fun configure() = Unit

    @Suppress("UNUSED_PARAMETER")
    fun watch(watchedObject: Any, description: String) = Unit

    fun status(): String = "LeakCanary release: not bundled; use debug build for benchmark reports"
}
