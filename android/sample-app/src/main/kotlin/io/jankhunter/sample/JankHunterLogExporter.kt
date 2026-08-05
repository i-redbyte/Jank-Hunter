package io.jankhunter.sample

import android.app.Activity
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.core.content.FileProvider
import io.jankhunter.runtime.JankHunter
import java.io.File
import java.util.Locale
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

internal class JankHunterLogExporter(
    private val context: Context,
    private val sourceDirectory: File = File(context.filesDir, JANK_HUNTER_DIRECTORY),
    private val exportDirectory: File = File(context.cacheDir, EXPORT_DIRECTORY),
) {
    fun createArchive(): File? {
        val artifacts = sourceDirectory
            .walkTopDown()
            .filter(File::isFile)
            .filter(::isDiagnosticArtifact)
            .sortedBy { it.relativeTo(sourceDirectory).invariantSeparatorsPath }
            .toList()
        if (artifacts.isEmpty()) return null

        exportDirectory.mkdirs()
        val archive = File(exportDirectory, "jank-hunter-${System.currentTimeMillis()}.zip")
        ZipOutputStream(archive.outputStream().buffered()).use { output ->
            artifacts.forEach { artifact ->
                val name = artifact.relativeTo(sourceDirectory).invariantSeparatorsPath
                output.putNextEntry(ZipEntry(name).apply { time = artifact.lastModified() })
                artifact.inputStream().buffered().use { input ->
                    input.copyTo(output, COPY_BUFFER_BYTES)
                }
                output.closeEntry()
            }
        }
        return archive
    }

    fun shareIntent(archive: File): Intent {
        val uri = archiveUri(archive)
        return Intent(Intent.ACTION_SEND).apply {
            type = ARCHIVE_MIME_TYPE
            putExtra(Intent.EXTRA_STREAM, uri)
            clipData = ClipData.newUri(context.contentResolver, archive.name, uri)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
    }

    private fun archiveUri(archive: File): Uri {
        return FileProvider.getUriForFile(
            context,
            "${context.packageName}.jankhunter.files",
            archive,
        )
    }

    private fun isDiagnosticArtifact(file: File): Boolean {
        return file.extension.lowercase(Locale.US) in DIAGNOSTIC_EXTENSIONS
    }

    private companion object {
        const val JANK_HUNTER_DIRECTORY = "jankhunter"
        const val EXPORT_DIRECTORY = "jankhunter-share"
        const val ARCHIVE_MIME_TYPE = "application/zip"
        const val COPY_BUFFER_BYTES = 64 * 1024
        val DIAGNOSTIC_EXTENSIONS = setOf("jhlog", "hprof")
    }
}

internal class JankHunterShareLauncher(
    private val activity: Activity,
) {
    fun share() {
        JankHunter.flush()
        Toast.makeText(activity, R.string.share_preparing, Toast.LENGTH_SHORT).show()
        Thread(
            {
                val exporter = JankHunterLogExporter(activity.applicationContext)
                val archive = runCatching { exporter.createArchive() }.getOrNull()
                activity.runOnUiThread {
                    if (activity.isFinishing || activity.isDestroyed) return@runOnUiThread
                    if (archive == null) {
                        Toast.makeText(activity, R.string.share_no_artifacts, Toast.LENGTH_LONG).show()
                    } else {
                        val chooser = Intent.createChooser(
                            exporter.shareIntent(archive),
                            activity.getString(R.string.share_chooser_title),
                        )
                        activity.startActivity(chooser)
                    }
                }
            },
            "JankHunterShareArchive",
        ).start()
    }
}
