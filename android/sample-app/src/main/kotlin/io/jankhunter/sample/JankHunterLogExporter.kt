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
    private val captureCurrentLogPaths: () -> List<String>? = {
        JankHunter.captureLogSnapshot()?.logPaths
    },
) {
    fun createArchive(): File? {
        val snapshotPaths = captureCurrentLogPaths() ?: return null
        val sealedLogs = snapshotPaths.mapTo(hashSetOf()) { path ->
            runCatching { File(path).canonicalPath }.getOrElse { File(path).absolutePath }
        }
        val artifacts = sourceDirectory
            .walkTopDown()
            .filter(File::isFile)
            .filter { file -> isDiagnosticArtifact(file, sealedLogs) }
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

    private fun isDiagnosticArtifact(file: File, sealedLogs: Set<String>): Boolean {
        return when (file.extension.lowercase(Locale.US)) {
            JHLOG_EXTENSION -> runCatching { file.canonicalPath }.getOrElse { file.absolutePath } in sealedLogs
            HPROF_EXTENSION -> true
            else -> false
        }
    }

    private companion object {
        const val JANK_HUNTER_DIRECTORY = "jankhunter"
        const val EXPORT_DIRECTORY = "jankhunter-share"
        const val ARCHIVE_MIME_TYPE = "application/zip"
        const val COPY_BUFFER_BYTES = 64 * 1024
        const val JHLOG_EXTENSION = "jhlog"
        const val HPROF_EXTENSION = "hprof"
    }
}

internal class JankHunterShareLauncher(
    private val activity: Activity,
) {
    fun share() {
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
