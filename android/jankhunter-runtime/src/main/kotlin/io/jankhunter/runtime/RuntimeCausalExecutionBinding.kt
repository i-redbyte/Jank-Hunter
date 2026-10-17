package io.jankhunter.runtime

/** Controlled wrapper run boundary, never inferred from matching names or TIDs. */
internal interface RuntimeCausalExecutionBinding {
    val recurring: Boolean
    fun enter(): Any?
    fun exit(token: Any?, failed: Boolean)
}
