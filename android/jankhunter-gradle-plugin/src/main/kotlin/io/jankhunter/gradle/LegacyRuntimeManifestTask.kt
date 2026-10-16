package io.jankhunter.gradle

import java.io.File
import javax.xml.XMLConstants
import javax.xml.parsers.DocumentBuilderFactory
import javax.xml.transform.TransformerFactory
import javax.xml.transform.dom.DOMSource
import javax.xml.transform.stream.StreamResult
import org.gradle.api.DefaultTask
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction
import org.w3c.dom.Element

/** AGP 8.0 has no generated manifest source API; transform its public merged artifact. */
@CacheableTask
abstract class LegacyRuntimeManifestTask : DefaultTask() {
    @get:InputFile @get:PathSensitive(PathSensitivity.NONE) abstract val inputManifest: RegularFileProperty
    @get:InputFile @get:PathSensitive(PathSensitivity.NONE) abstract val runtimeManifest: RegularFileProperty
    @get:Input abstract val applicationId: Property<String>
    @get:OutputFile abstract val outputManifest: RegularFileProperty

    @TaskAction fun merge() {
        mergeRuntimeManifest(inputManifest.get().asFile, runtimeManifest.get().asFile,
            outputManifest.get().asFile, applicationId.get())
    }
}

internal fun mergeRuntimeManifest(input: File, overlay: File, output: File, applicationId: String) {
    val factory = DocumentBuilderFactory.newInstance().apply {
        isNamespaceAware = true
        setFeature("http://apache.org/xml/features/disallow-doctype-decl", true)
        setFeature(XMLConstants.FEATURE_SECURE_PROCESSING, true)
    }
    val builder = factory.newDocumentBuilder()
    val document = builder.parse(input)
    val application = document.getElementsByTagName("application").item(0) as Element
    val additions = builder.parse(overlay).getElementsByTagName("application").item(0).childNodes
    for (index in 0 until additions.length) {
        val child = additions.item(index) as? Element ?: continue
        val name = child.getAttributeNS(ANDROID_NAMESPACE, "name")
        require(name.startsWith("io.jankhunter.")) { "Unowned runtime manifest entry" }
        val existing = application.childNodes
        for (position in existing.length - 1 downTo 0) {
            val element = existing.item(position) as? Element ?: continue
            if (element.tagName == child.tagName && element.getAttributeNS(ANDROID_NAMESPACE, "name") == name) {
                application.removeChild(element)
            }
        }
        val imported = document.importNode(child, true) as Element
        if (imported.hasAttributeNS(ANDROID_NAMESPACE, "authorities")) {
            imported.setAttributeNS(ANDROID_NAMESPACE, "android:authorities",
                imported.getAttributeNS(ANDROID_NAMESPACE, "authorities").replace("\${applicationId}", applicationId))
        }
        application.appendChild(imported)
    }
    check(output.parentFile.mkdirs() || output.parentFile.isDirectory)
    TransformerFactory.newInstance().apply { setFeature(XMLConstants.FEATURE_SECURE_PROCESSING, true) }
        .newTransformer().transform(DOMSource(document), StreamResult(output))
}

private const val ANDROID_NAMESPACE = "http://schemas.android.com/apk/res/android"
