package io.jankhunter.gradle

import org.objectweb.asm.Opcodes

/** Resolve an invocation before emitting bytecode; missing metadata never means permission to override. */
internal class LifecycleCallbackPlanner(private val index: LifecycleClassIndex) {
    fun plan(owner: String, callback: String): LifecycleCallbackPlan {
        val header = checkNotNull(index.header(owner)) { "Missing lifecycle class $owner" }
        val declaration = index.callback(owner, callback)
        return when (declaration) {
            is LifecycleCallbackResolution.MissingClass -> LifecycleCallbackPlan.Unsupported(
                "Missing hierarchy metadata for ${declaration.name} while resolving $owner.$callback",
            )
            LifecycleCallbackResolution.Absent -> LifecycleCallbackPlan.Unsupported("No declaration of $owner.$callback()V")
            is LifecycleCallbackResolution.Declaration -> when {
                declaration.access and (Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC) != 0 ->
                    LifecycleCallbackPlan.Unsupported("Non-virtual declaration ${declaration.owner}.$callback()V")
                declaration.owner == owner && declaration.hasBody -> LifecycleCallbackPlan.InstrumentDeclaration(owner)
                declaration.owner == owner -> LifecycleCallbackPlan.AbstractDeclaration
                declaration.canOverrideFrom(owner) && declaration.hasBody -> LifecycleCallbackPlan.Override(
                    checkNotNull(header.superName),
                    if (declaration.access and Opcodes.ACC_PUBLIC != 0) Opcodes.ACC_PUBLIC else Opcodes.ACC_PROTECTED,
                )
                declaration.hasBody && declaration.programClass &&
                    declaration.access and (Opcodes.ACC_PRIVATE or Opcodes.ACC_STATIC) == 0 ->
                    LifecycleCallbackPlan.InstrumentDeclaration(declaration.owner)
                header.access and Opcodes.ACC_ABSTRACT != 0 -> LifecycleCallbackPlan.AbstractDeclaration
                else -> LifecycleCallbackPlan.Unsupported(
                    "Cannot safely intercept $owner.$callback()V declared by ${declaration.owner} (access=${declaration.access})",
                )
            }
        }
    }
}

internal sealed interface LifecycleCallbackPlan {
    data class InstrumentDeclaration(val owner: String) : LifecycleCallbackPlan
    data class Override(val immediateParent: String, val access: Int) : LifecycleCallbackPlan
    data object AbstractDeclaration : LifecycleCallbackPlan
    data class Unsupported(val reason: String) : LifecycleCallbackPlan
}
