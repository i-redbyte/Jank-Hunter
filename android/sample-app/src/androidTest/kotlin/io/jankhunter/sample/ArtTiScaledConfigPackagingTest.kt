package io.jankhunter.sample

import android.content.pm.PackageManager
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ArtTiScaledConfigPackagingTest {
    @Test
    fun debugPackageIncludesScaledArtTiRuntimeAsset() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val lines = context.assets.open(SCALED_CONFIG_ASSET).bufferedReader().readLines()
            .map { it.trim() }
            .filter { it.isNotEmpty() }
        assertTrue("scaled ART TI asset must contain native options and trigger policy", lines.size >= 2)
        assertTrue(lines[0].contains("profile=2"))
        assertTrue(lines[1].startsWith("v=1;"))

        @Suppress("DEPRECATION")
        val manifestOptions = context.packageManager
            .getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
            .metaData
            ?.getString(META_NATIVE_OPTIONS)
            .orEmpty()
        assertTrue("manifest should still carry fallback ART TI metadata", manifestOptions.contains("profile=2"))
    }

    private companion object {
        const val SCALED_CONFIG_ASSET = "jankhunter/artti-runtime-config.txt"
        const val META_NATIVE_OPTIONS = "io.jankhunter.artti.native_options"
    }
}
