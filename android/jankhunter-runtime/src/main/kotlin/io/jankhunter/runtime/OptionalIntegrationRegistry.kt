package io.jankhunter.runtime

import android.content.Context
import android.content.pm.PackageManager

internal class OptionalIntegrationRegistry(
    private val discover: (Context) -> List<JankHunterRuntimeIntegration> = ::discoverFromManifest,
    private val eventSink: JankHunterAgentEventSink,
    private val diagnostic: (String) -> Unit,
) {
    @Volatile
    private var active: Array<JankHunterRuntimeIntegration> = emptyArray()

    fun startAll(context: Context) {
        if (active.isNotEmpty()) return
        val discovered = try {
            discover(context).take(MAX_INTEGRATIONS)
        } catch (_: Throwable) {
            diagnostic("discovery_failed")
            return
        }
        val started = ArrayList<JankHunterRuntimeIntegration>(discovered.size)
        val ids = HashSet<String>(discovered.size)
        discovered.forEach { integration ->
            val id = integration.id.take(MAX_ID_LENGTH)
            if (id.isBlank() || !ids.add(id)) {
                diagnostic("invalid_or_duplicate_id")
                return@forEach
            }
            try {
                integration.start(context, eventSink)
                started += integration
                diagnostic("$id.start_requested")
            } catch (_: Throwable) {
                diagnostic("$id.start_failed")
            }
        }
        active = started.toTypedArray()
    }

    fun stopAll(timeoutMs: Long) {
        val integrations = active
        active = emptyArray()
        if (integrations.isEmpty()) return
        val boundedTimeoutMs = timeoutMs.coerceIn(0L, Long.MAX_VALUE / NANOS_PER_MS)
        val deadlineNs = System.nanoTime() + boundedTimeoutMs * NANOS_PER_MS
        integrations.reversedArray().forEach { integration ->
            val remainingMs = ((deadlineNs - System.nanoTime()).coerceAtLeast(0L) / NANOS_PER_MS)
            try {
                integration.stop(remainingMs)
            } catch (_: Throwable) {
                diagnostic("${integration.id.take(MAX_ID_LENGTH)}.stop_failed")
            }
        }
    }

    fun onContextChanged(
        thread: Thread,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ) {
        active.forEach { integration ->
            try {
                integration.onContextChanged(thread, screen, owner, flow, step)
            } catch (_: Throwable) {
                diagnostic("${integration.id.take(MAX_ID_LENGTH)}.context_failed")
            }
        }
    }

    fun onMainThreadStall(thread: Thread, context: JankHunterContextSnapshot) {
        active.forEach { integration ->
            try {
                integration.onMainThreadStall(thread, context)
            } catch (_: Throwable) {
                diagnostic("${integration.id.take(MAX_ID_LENGTH)}.stall_trigger_failed")
            }
        }
    }

    internal fun activeCount(): Int = active.size

    internal fun hasActive(): Boolean = active.isNotEmpty()

    companion object {
        internal const val META_OPTIONAL_INTEGRATIONS = "io.jankhunter.runtime.optional_integrations"
        private const val MAX_INTEGRATIONS = 8
        private const val MAX_CLASS_NAME_LENGTH = 256
        private const val MAX_ID_LENGTH = 48
        private const val NANOS_PER_MS = 1_000_000L

        internal fun parseEntrypoints(raw: String?): List<String> {
            return raw.orEmpty()
                .split(',')
                .asSequence()
                .map(String::trim)
                .filter { it.isNotEmpty() && it.length <= MAX_CLASS_NAME_LENGTH && isClassName(it) }
                .distinct()
                .take(MAX_INTEGRATIONS)
                .toList()
        }

        private fun isClassName(value: String): Boolean {
            return value.split('.').all { part ->
                part.isNotEmpty() && (part[0].isLetter() || part[0] == '_') &&
                    part.drop(1).all { it.isLetterOrDigit() || it == '_' || it == '$' }
            }
        }

        @Suppress("DEPRECATION")
        private fun discoverFromManifest(context: Context): List<JankHunterRuntimeIntegration> {
            val metadata = context.packageManager
                .getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
                .metaData
            val raw = metadata?.get(META_OPTIONAL_INTEGRATIONS)?.toString()
            val loader = context.classLoader ?: JankHunterRuntimeIntegration::class.java.classLoader
            return parseEntrypoints(raw).mapNotNull { className ->
                try {
                    val type = Class.forName(className, true, loader)
                    if (!JankHunterRuntimeIntegration::class.java.isAssignableFrom(type)) return@mapNotNull null
                    @Suppress("UNCHECKED_CAST")
                    (type.getDeclaredConstructor().newInstance() as? JankHunterRuntimeIntegration)
                } catch (_: Throwable) {
                    null
                }
            }
        }
    }
}
