package io.jankhunter.gradle

import org.objectweb.asm.AnnotationVisitor

internal fun AnnotationVisitor.finishStringValue(value: String) {
    visit("value", value)
    visitEnd()
}
