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
    // Stable ABI tags describe the pre-R8 hierarchy, never the runtime class name.
    private val kind = hierarchy.firstNotNullOfOrNull(LifecycleTargetKind::fromRoot) ?: LifecycleTargetKind.NONE
    val targetKind: Int = kind.code

    val hookPoint: LifecycleHookPoint = select(
        enabled,
        constructor,
        staticMethod,
        methodName,
        methodDescriptor,
    )

    private fun select(
        enabled: Boolean,
        constructor: Boolean,
        staticMethod: Boolean,
        methodName: String,
        methodDescriptor: String,
    ): LifecycleHookPoint {
        if (!enabled || constructor || staticMethod || methodDescriptor != VOID_METHOD_DESCRIPTOR) {
            return LifecycleHookPoint.NONE
        }
        if (methodName == "onDestroyView") {
            return if (kind == LifecycleTargetKind.FRAGMENT) {
                LifecycleHookPoint.ENTER
            } else {
                LifecycleHookPoint.NONE
            }
        }
        val watched = when (methodName) {
            "onDestroy" -> kind == LifecycleTargetKind.ACTIVITY ||
                kind == LifecycleTargetKind.FRAGMENT || kind == LifecycleTargetKind.SERVICE
            "onCleared" -> kind == LifecycleTargetKind.VIEW_MODEL
            else -> false
        }
        return if (watched) LifecycleHookPoint.EXIT else LifecycleHookPoint.NONE
    }

    private companion object {
        private const val VOID_METHOD_DESCRIPTOR = "()V"
    }
}
