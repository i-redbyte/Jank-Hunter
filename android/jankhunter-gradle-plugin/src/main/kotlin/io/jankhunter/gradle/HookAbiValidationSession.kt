package io.jankhunter.gradle

import java.util.WeakHashMap

/** Execution-only headers per finalized AGP parameter object, discarded at build completion. */
internal object HookAbiValidationSession {
    private val indexes = WeakHashMap<JankHunterInstrumentationParameters, HookAbiIndex>()

    @Synchronized fun index(inputs: JankHunterInstrumentationParameters): HookAbiIndex {
        indexes[inputs]?.let { return it }
        val index = HookAbiValidation.validate(inputs.sdkCompileFiles.files, inputs.sdkRuntimeFiles.files,
            inputs.requireSdk.getOrElse(false), inputs.sdkRequiredOwners.getOrElse(emptySet()))
        if (indexes.size >= 64) indexes.remove(indexes.keys.first())
        indexes[inputs] = index
        return index
    }

    @Synchronized fun clear() = indexes.clear()
}
