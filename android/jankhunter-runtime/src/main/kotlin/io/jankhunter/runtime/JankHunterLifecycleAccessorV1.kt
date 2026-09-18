package io.jankhunter.runtime

/** ABI emitted by the Gradle plugin before R8; applications should not implement it manually. */
interface JankHunterLifecycleAccessorV1 {
    fun jankHunterLifecycleKindV1(): Int
    fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?)
}

/** Receives an existing target during synchronous lifecycle observation. */
fun interface JankHunterLifecycleTargetSinkV1 {
    fun accept(target: Any?, ownerHint: String?)
}

/**
 * Explicit opt-in for custom delegates; called at the fragment view destruction boundary.
 * Emit already-created bindings and their roots synchronously. Do not initialize a binding,
 * invoke a delegate with side effects, or retain the sink beyond this call.
 */
interface JankHunterBindingAccessor {
    fun visitJankHunterBindings(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?)
}
