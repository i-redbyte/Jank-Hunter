package io.jankhunter.gradle

/** Resolves method annotations over class defaults, including constructor inheritance rules. */
internal class MethodAnnotationContext(
    private val classAnnotations: JankAnnotationMetadata,
    private val constructor: Boolean,
    private val generatedOwnerLabel: String,
) {
    val methodAnnotations = JankAnnotationMetadata.Builder()

    val owner: String
        get() = methodAnnotations.owner?.takeIf { it.isNotBlank() } ?: classAnnotations.owner ?: generatedOwnerLabel

    val screen: String?
        get() = methodAnnotations.screen?.takeIf { it.isNotBlank() }
            ?: classAnnotations.screen.takeIf { !constructor || hasDirectConstructorContext }

    val operation: String?
        get() = methodAnnotations.operation?.takeIf { it.isNotBlank() }
            ?: classAnnotations.operation.takeIf { !constructor || hasDirectConstructorContext }

    val operationKind: String
        get() = if (methodAnnotations.operation?.isNotBlank() == true) {
            methodAnnotations.operationKind
        } else {
            classAnnotations.operationKind
        }

    val operationBudgetMs: Long
        get() = if (methodAnnotations.operation?.isNotBlank() == true) {
            methodAnnotations.operationBudgetMs
        } else {
            classAnnotations.operationBudgetMs
        }

    val hasContext: Boolean
        get() = screen != null ||
            methodAnnotations.owner?.takeIf { it.isNotBlank() } != null ||
            classAnnotations.owner != null && (!constructor || hasDirectConstructorContext)

    val ignored: Boolean
        get() = classAnnotations.ignored || methodAnnotations.ignored

    val composable: Boolean
        get() = methodAnnotations.composable

    private val hasDirectConstructorContext: Boolean
        get() = methodAnnotations.screen?.takeIf { it.isNotBlank() } != null ||
            methodAnnotations.operation?.takeIf { it.isNotBlank() } != null ||
            methodAnnotations.owner?.takeIf { it.isNotBlank() } != null

    fun diagnosticKey(): AnnotationDiagnosticKey? {
        if (!hasContext && operation == null) return null
        return AnnotationDiagnosticKey(
            owner = owner,
            screen = screen,
            operation = operation,
            operationKind = operationKind.takeIf { operation != null },
            operationBudgetMs = operationBudgetMs,
        )
    }
}
