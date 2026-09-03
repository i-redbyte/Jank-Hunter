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
    val detail: String? = null,
)

internal data class AnnotationDiagnosticKey(
    val owner: String?,
    val screen: String?,
    val operation: String?,
    val operationKind: String?,
    val operationBudgetMs: Long,
)

internal data class InstrumentationDiagnosticsRecord(
    val className: String,
    val methods: Int,
    val skippedMethods: Map<String, Int>,
    val ignoredMethods: Int,
    val annotatedMethods: Int,
    val methodFilterIncluded: Int,
    val methodFilterExcluded: Int,
    val methodFilterReasons: Map<String, Int>,
    val hooks: Map<HookDiagnosticKey, Int>,
    val decisions: Map<DecisionDiagnosticKey, Int>,
    val annotations: Map<AnnotationDiagnosticKey, Int>,
)

internal class InstrumentationDiagnosticsClassBuilder(
    private val className: String,
) {
    private var methods = 0
    private var ignoredMethods = 0
    private var methodFilterIncluded = 0
    private var methodFilterExcluded = 0
    private val methodFilterReasons = linkedMapOf<String, Int>()
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

    fun recordMethodFilter(decision: MethodFilterDecision, excluded: Boolean) {
        if (excluded) methodFilterExcluded++ else methodFilterIncluded++
        if (decision.categories.isEmpty()) {
            recordMethodFilterReason("regular", excluded)
        } else {
            decision.categories.forEach { recordMethodFilterReason(it, excluded) }
        }
    }

    private fun recordMethodFilterReason(reason: String, excluded: Boolean) {
        val key = if (excluded) "excluded:$reason" else "included:$reason"
        methodFilterReasons[key] = (methodFilterReasons[key] ?: 0) + 1
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

    fun recordHierarchyResolutionFailures(failures: Map<ClassHierarchyResolutionFailure, Int>) {
        failures.forEach { (failure, count) ->
            val omitted = failure.errorType == CLASS_HIERARCHY_OMITTED_FAILURE_TYPE
            val key = DecisionDiagnosticKey(
                kind = "warning",
                module = "class_hierarchy",
                family = "metadata",
                reason = if (omitted) "metadata_failures_omitted" else "metadata_load_failed",
                method = failure.className,
                line = null,
                detail = if (omitted) {
                    failure.detail
                } else {
                    buildString {
                        append(failure.errorType)
                        failure.detail?.takeIf(String::isNotBlank)?.let {
                            append(": ")
                            append(it)
                        }
                    }
                },
            )
            decisions[key] = (decisions[key] ?: 0) + count
        }
    }

    fun finish(): InstrumentationDiagnosticsRecord {
        return InstrumentationDiagnosticsRecord(
            className = className.replace('/', '.'),
            methods = methods,
            skippedMethods = skippedMethods.toMap(),
            ignoredMethods = ignoredMethods,
            annotatedMethods = annotations.values.sum(),
            methodFilterIncluded = methodFilterIncluded,
            methodFilterExcluded = methodFilterExcluded,
            methodFilterReasons = methodFilterReasons.toMap(),
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
            append(escapeJsonString(record.className))
            append("\",\"methods\":")
            append(record.methods)
            append(",\"ignoredMethods\":")
            append(record.ignoredMethods)
            append(",\"annotatedMethods\":")
            append(record.annotatedMethods)
            append(",\"methodFilterIncluded\":")
            append(record.methodFilterIncluded)
            append(",\"methodFilterExcluded\":")
            append(record.methodFilterExcluded)
            append(",\"methodFilterReasons\":[")
            appendSkipped(record.methodFilterReasons)
            append("],\"skippedMethods\":[")
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
                append(escapeJsonString(entry.key))
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
                append(escapeJsonString(entry.key.intent))
                append("\",\"signature\":\"")
                append(escapeJsonString(entry.key.signature))
                append("\",\"count\":")
                append(entry.value)
                append(",\"method\":\"")
                append(escapeJsonString(entry.key.method))
                append('"')
                entry.key.bridge?.let {
                    append(",\"bridge\":\"")
                    append(escapeJsonString(it))
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
                append(escapeJsonString(entry.key.kind))
                append("\",\"module\":\"")
                append(escapeJsonString(entry.key.module))
                append("\",\"family\":\"")
                append(escapeJsonString(entry.key.family))
                append("\",\"reason\":\"")
                append(escapeJsonString(entry.key.reason))
                append("\",\"count\":")
                append(entry.value)
                append(",\"method\":\"")
                append(escapeJsonString(entry.key.method))
                append('"')
                entry.key.line?.let {
                    append(",\"line\":")
                    append(it)
                }
                entry.key.detail?.let {
                    append(",\"detail\":\"")
                    append(escapeJsonString(it))
                    append('"')
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
                fieldCount = appendOptionalString(fieldCount, "operation", entry.key.operation)
                fieldCount = appendOptionalString(fieldCount, "operationKind", entry.key.operationKind)
                if (entry.key.operationBudgetMs > 0L) {
                    if (fieldCount > 0) append(',')
                    append("\"operationBudgetMs\":")
                    append(entry.key.operationBudgetMs)
                    fieldCount++
                }
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
        append(escapeJsonString(value))
        append('"')
        return fieldCount + 1
    }

}
