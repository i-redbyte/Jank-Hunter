package io.jankhunter.gradle

internal data class HookDiagnosticKey(
    val intent: String,
    val signature: String,
    val bridge: String?,
    val method: String,
    val line: Int?,
)

internal data class DecisionDiagnosticKey(
    val kind: String,
    val module: String,
    val family: String,
    val reason: String,
    val method: String,
    val line: Int?,
)

internal data class AnnotationDiagnosticKey(
    val owner: String?,
    val screen: String?,
    val flow: String?,
    val trace: String?,
)

internal data class InstrumentationDiagnosticsRecord(
    val className: String,
    val methods: Int,
    val skippedMethods: Map<String, Int>,
    val ignoredMethods: Int,
    val annotatedMethods: Int,
    val hooks: Map<HookDiagnosticKey, Int>,
    val decisions: Map<DecisionDiagnosticKey, Int>,
    val annotations: Map<AnnotationDiagnosticKey, Int>,
)

internal class InstrumentationDiagnosticsClassBuilder(
    private val className: String,
) {
    private var methods = 0
    private var ignoredMethods = 0
    private val skippedMethods = linkedMapOf<String, Int>()
    private val hooks = linkedMapOf<HookDiagnosticKey, Int>()
    private val decisions = linkedMapOf<DecisionDiagnosticKey, Int>()
    private val annotations = linkedMapOf<AnnotationDiagnosticKey, Int>()

    fun recordSkippedMethod(reason: String) {
        methods += 1
        skippedMethods[reason] = (skippedMethods[reason] ?: 0) + 1
    }

    fun recordMethod(ignored: Boolean, annotation: AnnotationDiagnosticKey?) {
        methods += 1
        if (ignored) {
            ignoredMethods += 1
        }
        if (annotation != null) {
            annotations[annotation] = (annotations[annotation] ?: 0) + 1
        }
    }

    fun recordHook(decision: HookDecision.Matched, method: String, line: Int?) {
        val key = HookDiagnosticKey(
            intent = decision.intent.id,
            signature = decision.signatureId,
            bridge = decision.bridgeId,
            method = method,
            line = line,
        )
        hooks[key] = (hooks[key] ?: 0) + 1
    }

    fun recordLifecycleHook(methodName: String, descriptor: String, superName: String?) {
        val key = HookDiagnosticKey(
            intent = "lifecycle.watch_retained",
            signature = "android.lifecycle.$methodName$descriptor",
            bridge = superName?.takeIf { it.isNotBlank() },
            method = "$methodName$descriptor",
            line = null,
        )
        hooks[key] = (hooks[key] ?: 0) + 1
    }

    fun recordDecision(decision: HookDecision, method: String, line: Int?) {
        val key = when (decision) {
            is HookDecision.Disabled -> DecisionDiagnosticKey(
                kind = "disabled",
                module = decision.moduleId,
                family = decision.family,
                reason = decision.reason,
                method = method,
                line = line,
            )
            is HookDecision.Unsupported -> DecisionDiagnosticKey(
                kind = "unsupported",
                module = decision.moduleId,
                family = decision.family,
                reason = decision.reason,
                method = method,
                line = line,
            )
            is HookDecision.Skipped -> DecisionDiagnosticKey(
                kind = "skipped",
                module = decision.moduleId,
                family = decision.family,
                reason = decision.reason,
                method = method,
                line = line,
            )
            is HookDecision.Matched,
            HookDecision.NotMatched -> return
        }
        decisions[key] = (decisions[key] ?: 0) + 1
    }

    fun finish(): InstrumentationDiagnosticsRecord {
        return InstrumentationDiagnosticsRecord(
            className = className.replace('/', '.'),
            methods = methods,
            skippedMethods = skippedMethods.toMap(),
            ignoredMethods = ignoredMethods,
            annotatedMethods = annotations.values.sum(),
            hooks = hooks.toMap(),
            decisions = decisions.toMap(),
            annotations = annotations.toMap(),
        )
    }
}

internal object InstrumentationDiagnosticsWriter {
    fun write(directoryPath: String, record: InstrumentationDiagnosticsRecord) {
        if (directoryPath.isBlank()) return
        InstrumentationArtifactFiles.writeClassShard(directoryPath, record.className, toJsonLine(record))
    }

    private fun toJsonLine(record: InstrumentationDiagnosticsRecord): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.INSTRUMENTATION_DIAGNOSTICS_FORMAT)
            append(",\"class\":\"")
            append(escape(record.className))
            append("\",\"methods\":")
            append(record.methods)
            append(",\"ignoredMethods\":")
            append(record.ignoredMethods)
            append(",\"annotatedMethods\":")
            append(record.annotatedMethods)
            append(",\"skippedMethods\":[")
            appendSkipped(record.skippedMethods)
            append("],\"hooks\":[")
            appendHooks(record.hooks)
            append("],\"decisions\":[")
            appendDecisions(record.decisions)
            append("],\"annotations\":[")
            appendAnnotations(record.annotations)
            append("]}\n")
        }
    }

    private fun StringBuilder.appendSkipped(skipped: Map<String, Int>) {
        skipped.entries
            .sortedWith(compareByDescending<Map.Entry<String, Int>> { it.value }.thenBy { it.key })
            .forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append("{\"reason\":\"")
                append(escape(entry.key))
                append("\",\"count\":")
                append(entry.value)
                append('}')
            }
    }

    private fun StringBuilder.appendHooks(hooks: Map<HookDiagnosticKey, Int>) {
        hooks.entries
            .sortedWith(
                compareByDescending<Map.Entry<HookDiagnosticKey, Int>> { it.value }
                    .thenBy { it.key.intent }
                    .thenBy { it.key.method }
                    .thenBy { it.key.line ?: Int.MAX_VALUE },
            )
            .forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append("{\"intent\":\"")
                append(escape(entry.key.intent))
                append("\",\"signature\":\"")
                append(escape(entry.key.signature))
                append("\",\"count\":")
                append(entry.value)
                append(",\"method\":\"")
                append(escape(entry.key.method))
                append('"')
                entry.key.bridge?.let {
                    append(",\"bridge\":\"")
                    append(escape(it))
                    append('"')
                }
                entry.key.line?.let {
                    append(",\"line\":")
                    append(it)
                }
                append('}')
            }
    }

    private fun StringBuilder.appendDecisions(decisions: Map<DecisionDiagnosticKey, Int>) {
        decisions.entries
            .sortedWith(
                compareByDescending<Map.Entry<DecisionDiagnosticKey, Int>> { it.value }
                    .thenBy { it.key.kind }
                    .thenBy { it.key.module }
                    .thenBy { it.key.method }
                    .thenBy { it.key.line ?: Int.MAX_VALUE },
            )
            .forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append("{\"kind\":\"")
                append(escape(entry.key.kind))
                append("\",\"module\":\"")
                append(escape(entry.key.module))
                append("\",\"family\":\"")
                append(escape(entry.key.family))
                append("\",\"reason\":\"")
                append(escape(entry.key.reason))
                append("\",\"count\":")
                append(entry.value)
                append(",\"method\":\"")
                append(escape(entry.key.method))
                append('"')
                entry.key.line?.let {
                    append(",\"line\":")
                    append(it)
                }
                append('}')
            }
    }

    private fun StringBuilder.appendAnnotations(annotations: Map<AnnotationDiagnosticKey, Int>) {
        annotations.entries
            .sortedWith(compareByDescending<Map.Entry<AnnotationDiagnosticKey, Int>> { it.value }.thenBy { it.key.owner })
            .forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append('{')
                var fieldCount = 0
                fieldCount = appendOptionalString(fieldCount, "owner", entry.key.owner)
                fieldCount = appendOptionalString(fieldCount, "screen", entry.key.screen)
                fieldCount = appendOptionalString(fieldCount, "flow", entry.key.flow)
                fieldCount = appendOptionalString(fieldCount, "trace", entry.key.trace)
                if (fieldCount > 0) append(',')
                append("\"count\":")
                append(entry.value)
                append('}')
            }
    }

    private fun StringBuilder.appendOptionalString(fieldCount: Int, name: String, value: String?): Int {
        if (value.isNullOrBlank()) return fieldCount
        if (fieldCount > 0) append(',')
        append('"')
        append(name)
        append("\":\"")
        append(escape(value))
        append('"')
        return fieldCount + 1
    }

    private fun escape(value: String): String {
        return value
            .replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\r", "\\r")
    }
}
