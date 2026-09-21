package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class MethodAnnotationContextTest {
    @Test
    fun constructorDoesNotInheritClassScopeWithoutADirectMethodAnnotation() {
        val context = MethodAnnotationContext(
            classAnnotations = JankAnnotationMetadata(
                owner = "Feature",
                screen = "Checkout",
                operation = "Create",
            ),
            constructor = true,
            generatedOwnerLabel = "example.Type.<init>",
        )

        assertNull(context.screen)
        assertNull(context.operation)
        assertFalse(context.hasContext)

        context.methodAnnotations.owner = "Constructor"

        assertEquals("Checkout", context.screen)
        assertEquals("Create", context.operation)
        assertTrue(context.hasContext)
        assertEquals("Constructor", context.owner)
    }
}
