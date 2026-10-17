package io.jankhunter.runtime

/** Applies explicit runtime changes to the last accepted immutable Jank Hunter configuration. */
fun interface JankHunterConfigUpdater {
    fun update(builder: JankHunterConfig.Builder)
}
