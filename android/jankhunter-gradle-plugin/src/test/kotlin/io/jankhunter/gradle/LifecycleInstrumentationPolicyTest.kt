package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test

class LifecycleInstrumentationPolicyTest {
    @Test
    fun fragmentViewIsCapturedBeforeDestroyViewAndFragmentAfterDestroy() {
        val hierarchy = setOf("androidx/fragment/app/Fragment")

        assertEquals(
            LifecycleHookPoint.ENTER,
            LifecycleInstrumentationPolicy(true, false, false, "onDestroyView", "()V", hierarchy).hookPoint,
        )
        assertEquals(
            LifecycleHookPoint.EXIT,
            LifecycleInstrumentationPolicy(true, false, false, "onDestroy", "()V", hierarchy).hookPoint,
        )
    }

    @Test
    fun staticConstructorAndUnsupportedDescriptorAreNeverInstrumented() {
        val hierarchy = setOf("android/app/Activity")

        assertEquals(
            LifecycleHookPoint.NONE,
            LifecycleInstrumentationPolicy(true, true, false, "onDestroy", "()V", hierarchy).hookPoint,
        )
        assertEquals(
            LifecycleHookPoint.NONE,
            LifecycleInstrumentationPolicy(true, false, true, "onDestroy", "()V", hierarchy).hookPoint,
        )
        assertEquals(
            LifecycleHookPoint.NONE,
            LifecycleInstrumentationPolicy(true, false, false, "onDestroy", "(I)V", hierarchy).hookPoint,
        )
    }
}
