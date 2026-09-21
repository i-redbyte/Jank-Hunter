package io.jankhunter.runtime

/** Applies runtime overrides to the immutable build-time Jank Hunter configuration. */
fun interface JankHunterConfigUpdater {
    fun update(builder: JankHunterConfig.Builder)
}
