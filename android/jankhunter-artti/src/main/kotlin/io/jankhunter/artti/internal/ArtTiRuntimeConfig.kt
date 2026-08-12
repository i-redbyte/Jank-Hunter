package io.jankhunter.artti.internal

import android.content.Context
import android.content.pm.PackageManager

internal data class ArtTiTriggerPolicy(
    val onMainThreadStall: Boolean,
    val onLongContention: Boolean,
    val minTriggerIntervalMs: Long,
    val maxSamplesPerMinute: Int,
    val drainIntervalMs: Long,
)

internal data class ArtTiRuntimeConfig(
    val native: ArtTiNativeConfig,
    val agentOptions: String,
    val triggerPolicy: ArtTiTriggerPolicy,
)

internal object ArtTiRuntimeConfigParser {
    const val META_NATIVE_OPTIONS = "io.jankhunter.artti.native_options"
    const val META_TRIGGER_POLICY = "io.jankhunter.artti.trigger_policy"

    fun fromManifest(context: Context): Result<ArtTiRuntimeConfig> = runCatching {
        @Suppress("DEPRECATION")
        val metadata = context.packageManager
            .getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
            .metaData
        parse(
            metadataValue(metadata, META_NATIVE_OPTIONS)?.toString(),
            metadataValue(metadata, META_TRIGGER_POLICY)?.toString(),
        ).getOrThrow()
    }

    internal fun parse(nativeOptions: String?, triggerPolicy: String?): Result<ArtTiRuntimeConfig> = runCatching {
        val nativeText = nativeOptions?.takeIf { it.length in 1..MAX_NATIVE_OPTIONS_LENGTH }
            ?: error("missing_or_oversized_native_options")
        val nativeFields = fields(nativeText, MAX_NATIVE_FIELDS)
        require(nativeFields.unsigned("v") == 1L) { "unsupported_native_options_version" }
        val native = ArtTiNativeConfig(
            profile = nativeFields.int("profile"),
            transportCapacity = nativeFields.int("transport"),
            maxTrackedThreads = nativeFields.int("threads"),
            maxOpenContentions = nativeFields.int("contentions"),
            maxStackDepth = nativeFields.int("depth"),
            maxStackDefinitions = nativeFields.int("stackdefs"),
            maxMethodDefinitions = nativeFields.int("methoddefs"),
            minStackTriggerIntervalMs = nativeFields.int("triggerms"),
            maxStackSamplesPerMinute = nativeFields.int("samplespm"),
            drainBatchSize = nativeFields.int("batch"),
            minContentionDurationNs = nativeFields.signedLong("mincontentionns"),
            configHash = nativeFields.unsigned("hash"),
            requestedCapabilities = nativeFields.unsigned("cap"),
        )
        require(native.validate() == ArtTiNativeStatus.OK) { "invalid_native_options" }

        val policyText = triggerPolicy?.takeIf { it.length in 1..MAX_POLICY_LENGTH }
            ?: error("missing_or_oversized_trigger_policy")
        val policyFields = fields(policyText, MAX_POLICY_FIELDS)
        require(policyFields.unsigned("v") == 1L) { "unsupported_trigger_policy_version" }
        val policy = ArtTiTriggerPolicy(
            onMainThreadStall = policyFields.boolean("main"),
            onLongContention = policyFields.boolean("long"),
            minTriggerIntervalMs = policyFields.signedLong("minms"),
            maxSamplesPerMinute = policyFields.int("maxpm"),
            drainIntervalMs = policyFields.signedLong("drainms"),
        )
        require(policy.minTriggerIntervalMs in 0L..60_000L) { "invalid_min_trigger_interval" }
        require(policy.maxSamplesPerMinute in 1..10_000) { "invalid_max_samples" }
        require(policy.drainIntervalMs in 10L..5_000L) { "invalid_drain_interval" }
        require(policy.minTriggerIntervalMs == native.minStackTriggerIntervalMs.toLong()) {
            "native_policy_trigger_interval_mismatch"
        }
        require(policy.maxSamplesPerMinute == native.maxStackSamplesPerMinute) {
            "native_policy_sample_budget_mismatch"
        }
        ArtTiRuntimeConfig(native, nativeText, policy)
    }

    private fun fields(value: String, maximum: Int): Map<String, String> {
        val result = LinkedHashMap<String, String>()
        value.split(';').forEach { field ->
            require(result.size < maximum) { "too_many_config_fields" }
            val separator = field.indexOf('=')
            require(separator in 1 until field.lastIndex) { "invalid_config_field" }
            val key = field.substring(0, separator)
            val fieldValue = field.substring(separator + 1)
            require(key.all { it in 'a'..'z' } && result.put(key, fieldValue) == null) {
                "invalid_or_duplicate_config_key"
            }
        }
        return result
    }

    private fun Map<String, String>.unsigned(key: String): Long {
        val raw = get(key) ?: error("missing_$key")
        return if (raw.startsWith("0x")) {
            java.lang.Long.parseUnsignedLong(raw.substring(2), 16)
        } else {
            java.lang.Long.parseUnsignedLong(raw, 10)
        }
    }

    private fun Map<String, String>.signedLong(key: String): Long {
        return (get(key) ?: error("missing_$key")).toLong()
    }

    private fun Map<String, String>.int(key: String): Int {
        val value = unsigned(key)
        require(value in 0L..Int.MAX_VALUE.toLong()) { "invalid_$key" }
        return value.toInt()
    }

    private fun Map<String, String>.boolean(key: String): Boolean {
        return when (unsigned(key)) {
            0L -> false
            1L -> true
            else -> error("invalid_$key")
        }
    }

    @Suppress("DEPRECATION")
    private fun metadataValue(metadata: android.os.Bundle?, key: String): Any? = metadata?.get(key)

    private const val MAX_NATIVE_OPTIONS_LENGTH = 1_024
    private const val MAX_NATIVE_FIELDS = 20
    private const val MAX_POLICY_LENGTH = 256
    private const val MAX_POLICY_FIELDS = 8
}
