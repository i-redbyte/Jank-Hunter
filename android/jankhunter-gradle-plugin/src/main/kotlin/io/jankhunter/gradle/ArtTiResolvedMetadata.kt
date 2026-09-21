package io.jankhunter.gradle

import java.io.File

internal object ArtTiResolvedMetadata {
    fun resolve(
        configBlob: String,
        overridesBlob: String,
        diagnosticsFiles: Iterable<File>,
        storageLimitMiB: Int,
        scaleToApplicationSize: Boolean,
        gradleModuleCount: Int,
    ): Pair<String, String> {
        if (configBlob.isEmpty()) return "" to ""
        val base = ArtTiConfigCodec.decode(configBlob)
        val footprint = ArtTiInstrumentationFootprintReader.readFiles(
            diagnosticsFiles,
            gradleModuleCount,
        )
        val effective = ArtTiDynamicScaler.scale(
            base = base,
            footprint = footprint,
            storageLimitMiB = storageLimitMiB,
            scaleToApplicationSize = scaleToApplicationSize,
            overrides = ArtTiConfigCodec.decodeOverrides(overridesBlob),
        )
        return effective.nativeAgentOptions() to effective.triggerPolicy()
    }
}
