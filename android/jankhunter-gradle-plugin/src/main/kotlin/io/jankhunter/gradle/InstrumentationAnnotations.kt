package io.jankhunter.gradle

import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.Opcodes

internal data class JankAnnotationMetadata(
    val owner: String? = null,
    val operation: String? = null,
    val operationKind: String = "USER",
    val operationBudgetMs: Long = 0L,
    val screen: String? = null,
    val ignored: Boolean = false,
    val composable: Boolean = false,
) {
    class Builder {
        var owner: String? = null
        var operation: String? = null
        var operationKind: String = "USER"
        var operationBudgetMs: Long = 0L
        var screen: String? = null
        var ignored: Boolean = false
        var composable: Boolean = false

        fun snapshot(): JankAnnotationMetadata {
            return JankAnnotationMetadata(
                owner = owner?.takeIf { it.isNotBlank() },
                operation = operation?.takeIf { it.isNotBlank() },
                operationKind = operationKind,
                operationBudgetMs = operationBudgetMs.coerceAtLeast(0L),
                screen = screen?.takeIf { it.isNotBlank() },
                ignored = ignored,
                composable = composable,
            )
        }
    }
}

internal object JankAnnotationParser {
    fun visitorFor(
        descriptor: String,
        delegate: AnnotationVisitor?,
        metadata: JankAnnotationMetadata.Builder,
    ): AnnotationVisitor? {
        return when (descriptor) {
            OWNER_DESCRIPTOR -> StringValueAnnotationVisitor(delegate) { metadata.owner = it }
            OPERATION_DESCRIPTOR -> OperationAnnotationVisitor(delegate, metadata)
            SCREEN_DESCRIPTOR -> StringValueAnnotationVisitor(delegate) { metadata.screen = it }
            IGNORE_DESCRIPTOR -> {
                metadata.ignored = true
                delegate
            }
            COMPOSABLE_DESCRIPTOR -> {
                metadata.composable = true
                delegate
            }
            else -> delegate
        }
    }

    private const val OWNER_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterOwner;"
    private const val OPERATION_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterOperation;"
    private const val SCREEN_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterScreen;"
    private const val IGNORE_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterIgnore;"
    private const val COMPOSABLE_DESCRIPTOR = "Landroidx/compose/runtime/Composable;"
}

private class OperationAnnotationVisitor(
    delegate: AnnotationVisitor?,
    private val metadata: JankAnnotationMetadata.Builder,
) : AnnotationVisitor(Opcodes.ASM9, delegate) {
    override fun visit(name: String?, value: Any?) {
        when {
            (name == null || name == "value") && value is String -> metadata.operation = value
            name == "budgetMs" && value is Long -> metadata.operationBudgetMs = value
        }
        super.visit(name, value)
    }

    override fun visitEnum(name: String?, descriptor: String?, value: String?) {
        if (name == "kind" && descriptor == OPERATION_KIND_DESCRIPTOR && value != null) {
            metadata.operationKind = value
        }
        super.visitEnum(name, descriptor, value)
    }

    private companion object {
        const val OPERATION_KIND_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterOperationKind;"
    }
}

private class StringValueAnnotationVisitor(
    delegate: AnnotationVisitor?,
    private val onValue: (String) -> Unit,
) : AnnotationVisitor(Opcodes.ASM9, delegate) {
    override fun visit(name: String?, value: Any?) {
        if ((name == null || name == "value") && value is String) {
            onValue(value)
        }
        super.visit(name, value)
    }
}
