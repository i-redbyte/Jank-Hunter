package io.jankhunter.gradle

import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.Opcodes

internal data class JankAnnotationMetadata(
    val owner: String? = null,
    val trace: String? = null,
    val tracePresent: Boolean = false,
    val flow: String? = null,
    val screen: String? = null,
    val ignored: Boolean = false,
) {
    class Builder {
        var owner: String? = null
        var trace: String? = null
        var tracePresent: Boolean = false
        var flow: String? = null
        var screen: String? = null
        var ignored: Boolean = false

        fun snapshot(): JankAnnotationMetadata {
            return JankAnnotationMetadata(
                owner = owner?.takeIf { it.isNotBlank() },
                trace = trace?.takeIf { it.isNotBlank() },
                tracePresent = tracePresent,
                flow = flow?.takeIf { it.isNotBlank() },
                screen = screen?.takeIf { it.isNotBlank() },
                ignored = ignored,
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
            TRACE_DESCRIPTOR -> {
                metadata.tracePresent = true
                StringValueAnnotationVisitor(delegate) { metadata.trace = it }
            }
            FLOW_DESCRIPTOR -> StringValueAnnotationVisitor(delegate) { metadata.flow = it }
            SCREEN_DESCRIPTOR -> StringValueAnnotationVisitor(delegate) { metadata.screen = it }
            IGNORE_DESCRIPTOR -> {
                metadata.ignored = true
                delegate
            }
            else -> delegate
        }
    }

    private const val OWNER_DESCRIPTOR = "Lio/jankhunter/annotations/JankOwner;"
    private const val TRACE_DESCRIPTOR = "Lio/jankhunter/annotations/JankTrace;"
    private const val FLOW_DESCRIPTOR = "Lio/jankhunter/annotations/JankFlow;"
    private const val SCREEN_DESCRIPTOR = "Lio/jankhunter/annotations/JankScreen;"
    private const val IGNORE_DESCRIPTOR = "Lio/jankhunter/annotations/JankIgnore;"
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
