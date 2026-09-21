package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

class GenerateJankHunterArtTiScaledConfigTaskTest {
    @Test
    fun writesScaledNativeOptionsAndTriggerPolicyAsset() {
        val project = ProjectBuilder.builder().build()
        val diagnostics = File(project.projectDir, "diagnostics.jsonl")
        diagnostics.writeText(
            """
            {"class":"com.example.A","methods":12,"pass":"asm","hooks":[{"count":3}]}
            {"class":"com.example.B","methods":8,"pass":"lifecycle","hooks":[{"count":1}]}
            """.trimIndent(),
        )
        val task = project.tasks.register(
            "artTiScaled",
            GenerateJankHunterArtTiScaledConfigTask::class.java,
        ).get()
        val artTiDsl = project.objects.newInstance(JankHunterExtension.ArtTi::class.java).apply {
            mode.set(ArtTiMode.CAUSAL)
        }
        val base = EffectiveArtTiConfigResolver.resolve(artTiDsl)
        task.artTiConfigBlob.set(ArtTiConfigCodec.encode(base))
        task.artTiExplicitOverridesBlob.set(ArtTiConfigCodec.encodeOverrides(ArtTiExplicitOverrides()))
        task.artTiScaleToApplicationSize.set(true)
        task.artTiStorageLimitMiB.set(256)
        task.artTiGradleModuleCount.set(4)
        task.diagnosticsFiles.from(diagnostics)
        task.assetsDirectory.set(project.layout.buildDirectory.dir("artti-assets"))

        task.writeAsset()

        val asset = task.assetsDirectory.file(GenerateJankHunterArtTiScaledConfigTask.ASSET_PATH).get().asFile
        val lines = asset.readText().trim().lines()
        assertEquals(2, lines.size)
        assertTrue(lines[0].contains("transport="))
        assertTrue(lines[1].startsWith("v=1;"))
        assertTrue(lines[0] != base.nativeAgentOptions())
    }
}
