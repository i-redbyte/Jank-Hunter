package io.jankhunter.annotations

@MustBeDocumented
@Retention(AnnotationRetention.BINARY)
@Target(AnnotationTarget.CLASS, AnnotationTarget.FUNCTION, AnnotationTarget.CONSTRUCTOR)
annotation class JankHunterOperation(
    val value: String,
    val kind: JankHunterOperationKind = JankHunterOperationKind.USER,
    val budgetMs: Long = 0L,
)

enum class JankHunterOperationKind {
    USER,
    SCREEN,
    BACKGROUND,
    SYSTEM,
    STAGE,
}
