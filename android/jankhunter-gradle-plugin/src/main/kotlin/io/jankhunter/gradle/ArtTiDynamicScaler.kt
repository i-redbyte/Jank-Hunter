package io.jankhunter.gradle

internal data class ArtTiExplicitOverrides(
    val transportCapacity: Boolean = false,
    val maxTrackedThreads: Boolean = false,
    val maxOpenContentions: Boolean = false,
    val maxStackDefinitions: Boolean = false,
    val maxMethodDefinitions: Boolean = false,
    val drainBatchSize: Boolean = false,
    val maxSamplesPerMinute: Boolean = false,
    val minTriggerIntervalMs: Boolean = false,
)

internal object ArtTiDynamicScaler {
    fun scale(
        base: EffectiveArtTiConfig,
        footprint: ArtTiInstrumentationFootprint?,
        storageLimitMiB: Int,
        scaleToApplicationSize: Boolean,
        overrides: ArtTiExplicitOverrides,
    ): EffectiveArtTiConfig {
        if (!scaleToApplicationSize || base.mode == ArtTiMode.OFF || base.mode == ArtTiMode.CUSTOM) {
            return base
        }
        val multiplier = scaleMultiplier(footprint, storageLimitMiB)
        if (multiplier == 1.0) {
            return base
        }
        val scaled = base.copy(
            maxTrackedThreads = scaleInt(base.maxTrackedThreads, multiplier, overrides.maxTrackedThreads, 1, 16_384),
            maxOpenContentions = scaleInt(base.maxOpenContentions, multiplier, overrides.maxOpenContentions, 1, 65_536),
            maxStackDefinitions = scaleInt(base.maxStackDefinitions, multiplier, overrides.maxStackDefinitions, 1, 65_536),
            maxMethodDefinitions = scaleInt(base.maxMethodDefinitions, multiplier, overrides.maxMethodDefinitions, 1, 262_144),
            transportCapacity = scalePowerOfTwo(base.transportCapacity, multiplier, overrides.transportCapacity, 2, 65_536),
            drainBatchSize = scaleInt(
                base.drainBatchSize,
                multiplier,
                overrides.drainBatchSize,
                1,
                minOf(base.transportCapacity, EffectiveArtTiConfigResolver.MAX_DRAIN_BATCH),
            ),
            maxSamplesPerMinute = scaleInt(
                base.maxSamplesPerMinute,
                multiplier,
                overrides.maxSamplesPerMinute,
                1,
                10_000,
            ),
        )
        val validated = scaled.copy(
            drainBatchSize = scaled.drainBatchSize.coerceAtMost(scaled.transportCapacity),
        )
        EffectiveArtTiConfigResolver.validateScaled(validated)
        return validated.copy(configHash = EffectiveArtTiConfigResolver.hash(validated))
    }

    internal fun scaleMultiplier(footprint: ArtTiInstrumentationFootprint?, storageLimitMiB: Int): Double {
        val score = footprint?.scaleScore ?: 128L
        val sizeTier = when {
            score < 256L -> 1.0
            score < 2_048L -> 1.5
            score < 8_192L -> 2.0
            else -> 4.0
        }
        val storageMiB = storageLimitMiB.coerceAtLeast(1)
        val storageTier = when {
            storageMiB < 64 -> 0.75
            storageMiB < 128 -> 1.0
            storageMiB < 256 -> 1.25
            else -> 1.5
        }
        return sizeTier * storageTier
    }

    private fun scaleInt(base: Int, multiplier: Double, explicit: Boolean, min: Int, max: Int): Int {
        if (explicit) return base
        val scaled = (base * multiplier).toInt().coerceIn(min, max)
        return scaled
    }

    private fun scalePowerOfTwo(base: Int, multiplier: Double, explicit: Boolean, min: Int, max: Int): Int {
        if (explicit) return base
        val target = (base * multiplier).toInt().coerceIn(min, max)
        return target.nextPowerOfTwo().coerceIn(min, max)
    }

    private fun Int.nextPowerOfTwo(): Int {
        var value = if (this <= 1) 1 else this - 1
        value = value or (value shr 1)
        value = value or (value shr 2)
        value = value or (value shr 4)
        value = value or (value shr 8)
        value = value or (value shr 16)
        return value + 1
    }
}
