package io.jankhunter.gradle

internal object ArtTiConfigCodec {
    private const val FIELD_SEPARATOR = '\u001f'

    fun encode(config: EffectiveArtTiConfig): String {
        return listOf(
            config.mode.name,
            if (config.garbageCollectionEnabled) "1" else "0",
            if (config.threadLifecycleEnabled) "1" else "0",
            config.maxTrackedThreads.toString(),
            if (config.monitorContentionEnabled) "1" else "0",
            config.minContentionDurationMs.toString(),
            config.maxOpenContentions.toString(),
            if (config.stackSamplingEnabled) "1" else "0",
            config.maxStackDepth.toString(),
            if (config.onMainThreadStall) "1" else "0",
            if (config.onLongContention) "1" else "0",
            config.minTriggerIntervalMs.toString(),
            config.maxSamplesPerMinute.toString(),
            config.maxStackDefinitions.toString(),
            config.maxMethodDefinitions.toString(),
            config.transportCapacity.toString(),
            config.drainBatchSize.toString(),
            config.overflowPolicy.name,
        ).joinToString(FIELD_SEPARATOR.toString())
    }

    fun decode(blob: String): EffectiveArtTiConfig {
        val fields = blob.split(FIELD_SEPARATOR)
        require(fields.size == 18) { "invalid ART TI config blob" }
        return EffectiveArtTiConfig(
            mode = ArtTiMode.valueOf(fields[0]),
            garbageCollectionEnabled = fields[1].toInt() != 0,
            threadLifecycleEnabled = fields[2].toInt() != 0,
            maxTrackedThreads = fields[3].toInt(),
            monitorContentionEnabled = fields[4].toInt() != 0,
            minContentionDurationMs = fields[5].toLong(),
            maxOpenContentions = fields[6].toInt(),
            stackSamplingEnabled = fields[7].toInt() != 0,
            maxStackDepth = fields[8].toInt(),
            onMainThreadStall = fields[9].toInt() != 0,
            onLongContention = fields[10].toInt() != 0,
            minTriggerIntervalMs = fields[11].toLong(),
            maxSamplesPerMinute = fields[12].toInt(),
            maxStackDefinitions = fields[13].toInt(),
            maxMethodDefinitions = fields[14].toInt(),
            transportCapacity = fields[15].toInt(),
            drainBatchSize = fields[16].toInt(),
            overflowPolicy = ArtTiOverflowPolicy.valueOf(fields[17]),
            configHash = 0L,
        )
    }

    fun encodeOverrides(overrides: ArtTiExplicitOverrides): String {
        return listOf(
            overrides.transportCapacity,
            overrides.maxTrackedThreads,
            overrides.maxOpenContentions,
            overrides.maxStackDefinitions,
            overrides.maxMethodDefinitions,
            overrides.drainBatchSize,
            overrides.maxSamplesPerMinute,
            overrides.minTriggerIntervalMs,
        ).joinToString(FIELD_SEPARATOR.toString()) { if (it) "1" else "0" }
    }

    fun decodeOverrides(blob: String): ArtTiExplicitOverrides {
        val fields = blob.split(FIELD_SEPARATOR)
        require(fields.size == 8) { "invalid ART TI override blob" }
        return ArtTiExplicitOverrides(
            transportCapacity = fields[0] == "1",
            maxTrackedThreads = fields[1] == "1",
            maxOpenContentions = fields[2] == "1",
            maxStackDefinitions = fields[3] == "1",
            maxMethodDefinitions = fields[4] == "1",
            drainBatchSize = fields[5] == "1",
            maxSamplesPerMinute = fields[6] == "1",
            minTriggerIntervalMs = fields[7] == "1",
        )
    }
}
