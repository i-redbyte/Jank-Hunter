package io.jankhunter.gradle

internal enum class LifecycleHookPoint {
    NONE,
    ENTER,
    EXIT,
}

internal class LifecycleInstrumentationPolicy(
    enabled: Boolean,
    constructor: Boolean,
    staticMethod: Boolean,
    methodName: String,
    methodDescriptor: String,
    hierarchy: Set<String>,
) {
    val hookPoint: LifecycleHookPoint = select(
        enabled,
        constructor,
        staticMethod,
        methodName,
        methodDescriptor,
        hierarchy,
    )

    private fun select(
        enabled: Boolean,
        constructor: Boolean,
        staticMethod: Boolean,
        methodName: String,
        methodDescriptor: String,
        hierarchy: Set<String>,
    ): LifecycleHookPoint {
        if (!enabled || constructor || staticMethod || methodDescriptor != VOID_METHOD_DESCRIPTOR) {
            return LifecycleHookPoint.NONE
        }
        if (methodName == "onDestroyView") {
            return if (ANDROIDX_FRAGMENT in hierarchy || ANDROID_FRAGMENT in hierarchy) {
                LifecycleHookPoint.ENTER
            } else {
                LifecycleHookPoint.NONE
            }
        }
        val watched = when (methodName) {
            "onDestroy" -> ANDROID_ACTIVITY in hierarchy ||
                ANDROIDX_FRAGMENT in hierarchy ||
                ANDROID_FRAGMENT in hierarchy ||
                ANDROID_SERVICE in hierarchy
            "onCleared" -> ANDROIDX_VIEW_MODEL in hierarchy
            else -> false
        }
        return if (watched) LifecycleHookPoint.EXIT else LifecycleHookPoint.NONE
    }

    private companion object {
        private const val VOID_METHOD_DESCRIPTOR = "()V"
        private const val ANDROID_ACTIVITY = "android/app/Activity"
        private const val ANDROID_FRAGMENT = "android/app/Fragment"
        private const val ANDROID_SERVICE = "android/app/Service"
        private const val ANDROIDX_FRAGMENT = "androidx/fragment/app/Fragment"
        private const val ANDROIDX_VIEW_MODEL = "androidx/lifecycle/ViewModel"
    }
}
