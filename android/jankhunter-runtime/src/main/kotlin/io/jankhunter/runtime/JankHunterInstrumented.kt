package io.jankhunter.runtime

/** Build-time idempotency marker emitted by the Jank Hunter ASM transform. */
@Target(AnnotationTarget.CLASS)
@Retention(AnnotationRetention.BINARY)
internal annotation class JankHunterInstrumented

/** Separate marker for the low-overhead lifecycle-only dependency transform. */
@Target(AnnotationTarget.CLASS)
@Retention(AnnotationRetention.BINARY)
internal annotation class JankHunterLifecycleInstrumented
