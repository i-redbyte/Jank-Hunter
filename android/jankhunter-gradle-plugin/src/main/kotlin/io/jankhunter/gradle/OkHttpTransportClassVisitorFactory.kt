package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import com.android.build.api.instrumentation.InstrumentationParameters
import org.gradle.api.provider.SetProperty
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.Input
import org.objectweb.asm.ClassVisitor
import java.io.File
import java.util.zip.ZipFile

interface OkHttpTransportInstrumentationParameters : InstrumentationParameters {
    @get:Input val excludePackages: SetProperty<String>
    @get:Classpath val supportClasspath: ConfigurableFileCollection
}

/** Transport observation has an exact dependency boundary, independent of application hooks. */
abstract class OkHttpTransportClassVisitorFactory : AsmClassVisitorFactory<OkHttpTransportInstrumentationParameters> {
    override fun isInstrumentable(classData: ClassData): Boolean =
        OkHttpTransportClassVisitor.matches(classData.className) &&
            !InstrumentationMarker.isPresent(classData.classAnnotations) &&
            parameters.get().excludePackages.getOrElse(emptySet()).none { excluded ->
                InstrumentationPackages.matchesPackageBoundary(classData.className, excluded)
            }

    override fun createClassVisitor(classContext: ClassContext, nextClassVisitor: ClassVisitor): ClassVisitor {
        // AGP's ClassContext resolves dependencies of the visited artifact. OkHttp cannot depend
        // on its consumer's helper, so resolve that capability from the application's classpath.
        // @Classpath keeps both project build dependencies and transform cache invalidation exact.
        val hasABI = parameters.get().supportClasspath.any(::containsTransportAbi)
        val hasReviewedCodec = classContext.loadClassData("okhttp3.internal.http1.Http1Codec") != null
        return if (hasABI && hasReviewedCodec) OkHttpTransportClassVisitor(nextClassVisitor) else nextClassVisitor
    }

    private fun containsTransportAbi(file: File): Boolean {
        val entry = OkHttpTransportClassVisitor.OWNER_ABI + ".class"
        return when {
            file.isDirectory -> File(file, entry).isFile
            file.extension == "jar" -> ZipFile(file).use { it.getEntry(entry) != null }
            else -> false
        }
    }
}
