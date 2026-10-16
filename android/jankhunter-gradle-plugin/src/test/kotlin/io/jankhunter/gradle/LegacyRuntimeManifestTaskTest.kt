package io.jankhunter.gradle

import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class LegacyRuntimeManifestTaskTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test fun overlayPreservesApplicationAndPermissionWhileResolvingProviderIdentity() {
        val input = temporary.newFile("input.xml").apply { writeText("""
            <manifest xmlns:android="http://schemas.android.com/apk/res/android" package="app.fixture">
              <permission android:name="app.fixture.signature" android:protectionLevel="signature"/>
              <application android:name="app.Custom"><activity android:name="app.Screen"/>
                <meta-data android:name="app.custom" android:value="keep"/>
                <meta-data android:name="io.jankhunter.enabled" android:value="false"/>
              </application>
            </manifest>
        """.trimIndent()) }
        val overlay = temporary.newFile("overlay.xml").apply { writeText("""
            <manifest xmlns:android="http://schemas.android.com/apk/res/android"><application>
              <meta-data android:name="io.jankhunter.enabled" android:value="true"/>
              <provider android:name="io.jankhunter.runtime.JankHunterAutoInitProvider"
                android:authorities="${'$'}{applicationId}.jankhunter-init" android:exported="false"/>
            </application></manifest>
        """.trimIndent()) }
        val output = temporary.newFile("output.xml")
        mergeRuntimeManifest(input, overlay, output, "app.fixture")
        val factory = DocumentBuilderFactory.newInstance().apply { isNamespaceAware = true }
        val document = factory.newDocumentBuilder().parse(output)
        assertEquals(1, document.getElementsByTagName("permission").length)
        assertEquals(1, document.getElementsByTagName("activity").length)
        assertEquals(2, document.getElementsByTagName("meta-data").length)
        val provider = document.getElementsByTagName("provider").item(0) as org.w3c.dom.Element
        assertEquals("app.fixture.jankhunter-init", provider.getAttributeNS("http://schemas.android.com/apk/res/android", "authorities"))
        assertFalse(output.readText().contains("applicationId"))
    }
}
