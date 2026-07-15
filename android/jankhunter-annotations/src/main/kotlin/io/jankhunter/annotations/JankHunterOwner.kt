package io.jankhunter.annotations

@MustBeDocumented
@Retention(AnnotationRetention.BINARY)
@Target(AnnotationTarget.CLASS, AnnotationTarget.FUNCTION, AnnotationTarget.CONSTRUCTOR)
annotation class JankHunterOwner(
    val value: String,
)
