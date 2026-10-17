package io.jankhunter.gradle

import java.io.File
import java.nio.file.Files
import java.util.TreeMap
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream
import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.Directory
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFile
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.ListProperty
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.tree.ClassNode

/** Runs after app compilation, so AGP dependency transforms never need a task-produced catalog. */
@CacheableTask
abstract class CausalProvenanceTransformTask : DefaultTask() {
    @get:Input @get:org.gradle.api.tasks.Optional abstract val sdkHookAbi: Property<HookAbiIndex>
    @get:Input abstract val metadataCompatibilityMode: Property<JankHunterMetadataCompatibilityMode>
    @get:Input abstract val metadataReaderIdentity: Property<String>
    @get:Classpath abstract val inputJars: ListProperty<RegularFile>
    @get:Classpath abstract val inputDirectories: ListProperty<Directory>
    @get:InputFile @get:PathSensitive(PathSensitivity.NONE)
    abstract val codeOriginCatalog: RegularFileProperty
    @get:Classpath abstract val supportClasspath: ConfigurableFileCollection
    @get:Input abstract val includeWholeApplication: Property<Boolean>
    @get:Input abstract val includePackages: SetProperty<String>
    @get:Input abstract val excludePackages: SetProperty<String>
    @get:Input abstract val executors: Property<Boolean>
    @get:Input abstract val handlers: Property<Boolean>
    @get:Input abstract val threadLifecycle: Property<Boolean>
    @get:Input abstract val concurrencyDiagnostics: Property<Boolean>
    @get:Input abstract val coroutines: Property<Boolean>
    @get:Input abstract val workerTracing: Property<Boolean>
    @get:OutputFile abstract val outputFile: RegularFileProperty
    @get:OutputDirectory abstract val diagnosticsDirectory: DirectoryProperty

    init {
        metadataCompatibilityMode.convention(JankHunterMetadataCompatibilityMode.COMPATIBILITY)
        metadataReaderIdentity.convention(MetadataReaderCapabilities.identity)
        includeWholeApplication.convention(false)
        diagnosticsDirectory.convention(project.layout.buildDirectory.dir("intermediates/jankhunter/$name/diagnostics"))
    }

    @TaskAction
    fun transform() {
        val diagnostics = diagnosticsDirectory.get().asFile
        if (diagnostics.exists()) check(diagnostics.deleteRecursively()) { "Cannot clear causal diagnostics $diagnostics" }
        check(diagnostics.mkdirs() || diagnostics.isDirectory)
        val origins = CausalCodeOriginCatalog.load(codeOriginCatalog.get().asFile)
        val opened = mutableListOf<ZipFile>()
        val scratch = Files.createTempDirectory(temporaryDir.toPath(), "causal-hierarchy-").toFile()
        try {
            LifecycleClassIndex(scratch.resolve("metadata.mv")).use { hierarchy ->
                val entries = TreeMap<String, EntrySource>()
                inputJars.get().forEach { input ->
                    val archive = ZipFile(input.asFile)
                    opened.add(archive)
                    archive.entries().asSequence().filterNot(ZipEntry::isDirectory).forEach { entry ->
                        add(entries, entry.name, EntrySource.Archive(archive, entry))
                    }
                }
                inputDirectories.get().forEach { input ->
                    val root = input.asFile.toPath()
                    Files.walk(root).use { files ->
                        files.filter(Files::isRegularFile).forEach { path ->
                            val name = root.relativize(path).toString().replace(File.separatorChar, '/')
                            add(entries, name, EntrySource.Disk(path.toFile()))
                        }
                    }
                }
                supportClasspath.files.forEach { input ->
                    LifecycleClassInputs.forEach(input) { hierarchy.add(it, programClass = false) }
                }
                entries.forEach { (name, source) ->
                    if (isProgramClass(name)) {
                        source.open().use { hierarchy.add(it.readBytes(), programClass = true) }
                    }
                }
                val resolver = CausalHierarchyResolver(hierarchy)
                val config = HookConfig(
                    methodCounters = false, methodFilterMode = JankHunterMethodFilterMode.NONE,
                    metadataCompatibilityMode = metadataCompatibilityMode.get(),
                    metadataDecodePathway = MetadataDecodePathway.CAUSAL,
                    okhttp = false, webSockets = false, handlers = handlers.get(),
                    executors = executors.get(), threadLifecycle = threadLifecycle.get(),
                    concurrencyDiagnostics = concurrencyDiagnostics.get(), coroutines = coroutines.get(),
                    interactionOperations = false, logSpam = false, classGraph = false,
                    runtimeCallGraph = false, classGraphDirectory = "", instrumentationDiagnosticsDirectory = diagnostics.absolutePath,
                    composeTracing = false, roomTracing = false, databaseTracing = false,
                    workerTracing = workerTracing.get(),
                )
                val included = includePackages.get()
                val excluded = excludePackages.get()
                val target = outputFile.get().asFile
                target.parentFile.mkdirs()
                ZipOutputStream(target.outputStream().buffered()).use { output ->
                    entries.forEach { (name, source) ->
                        val transformClass: ((ByteArray) -> ByteArray)? =
                            if (isProgramClass(name) && shouldInstrument(name, origins, included, excluded)) {
                                { bytes -> instrument(bytes, name.removeSuffix(".class"), origins, resolver, config) }
                            } else null
                        writeCausalArchiveEntry(output, name, source::open, transformClass)
                    }
                }
            }
        } finally {
            try {
                opened.forEach(ZipFile::close)
            } finally {
                if (!scratch.deleteRecursively()) logger.warn("Cannot remove causal temporary storage {}", scratch)
            }
        }
    }

    private fun shouldInstrument(
        path: String,
        origins: CausalCodeOriginCatalog,
        included: Set<String>,
        excluded: Set<String>,
    ): Boolean {
        val className = path.removeSuffix(".class")
        if (isJankHunterSupportClass(className)) return false
        val normalized = InstrumentationPackages.normalizePackage(className.replace('/', '.'))
        return InstrumentationMatcher.matchesNormalizedClassName(
            normalized, included, excluded, includeWholeApplication.get(),
        ) ||
            origins.origin(className) == CausalCodeOrigin.PROJECT &&
            InstrumentationMatcher.matchesNormalizedClassName(
                normalized, emptySet(), excluded, includeWholeApplication = true, includeBuiltinExcludes = false,
            )
    }

    private fun instrument(
        bytes: ByteArray,
        className: String,
        origins: CausalCodeOriginCatalog,
        resolver: CausalHierarchyResolver,
        config: HookConfig,
    ): ByteArray {
        val reader = ClassReader(bytes)
        val candidate = ClassNode()
        var diagnostics: InstrumentationDiagnosticsRecord? = null
        val visitor = JankHunterClassVisitor(
            sdkHookAbi.orNull?.visitor(candidate) ?: candidate, className, config, classOrigin = origins.origin(className),
            classHierarchy = resolver.resolve(className), resolveOwnerHierarchy = resolver::resolve,
            instrumentationMarkerDescriptor = "Lio/jankhunter/runtime/JankHunterCausalInstrumented;",
            markerOnlyWhenHookApplied = true, diagnosticsOnlyWhenHookApplied = true,
            diagnosticsSink = { diagnostics = it },
        )
        reader.accept(visitor, ClassReader.EXPAND_FRAMES)
        if (!visitor.hasAppliedHooks) {
            diagnostics?.let { InstrumentationDiagnosticsWriter.write(config.instrumentationDiagnosticsDirectory, it) }
            return bytes
        }
        val writer = object : ClassWriter(reader, COMPUTE_FRAMES or COMPUTE_MAXS) {
            override fun getCommonSuperClass(type1: String, type2: String): String = resolver.common(type1, type2)
        }
        val result = try {
            candidate.accept(writer)
            writer.toByteArray()
        } catch (missing: MissingFrameTypeException) {
            diagnostics = diagnostics?.withoutCausalHooks(missing.internalName)
            logger.warn("Jank Hunter: causal instrumentation skipped for {}; missing frame type {}", className, missing.internalName)
            bytes
        }
        diagnostics?.let { InstrumentationDiagnosticsWriter.write(config.instrumentationDiagnosticsDirectory, it) }
        return result
    }

    private fun InstrumentationDiagnosticsRecord.withoutCausalHooks(missing: String): InstrumentationDiagnosticsRecord {
        val warning = DecisionDiagnosticKey(
            kind = "unsupported", module = "class_hierarchy", family = "causal",
            reason = "missing_frame_type", method = className, line = null, detail = missing,
        )
        return copy(
            skippedMethods = mapOf("missing_frame_type" to methods),
            hooks = emptyMap(), decisions = decisions + (warning to 1),
            methodFilterIncluded = 0, methodFilterExcluded = 0, methodFilterReasons = emptyMap(),
        )
    }

    private fun add(entries: MutableMap<String, EntrySource>, name: String, source: EntrySource) {
        check(!name.startsWith('/') && name.split('/').none { it == ".." }) { "Invalid class entry '$name'" }
        val existing = entries.putIfAbsent(name, source)
        check(existing == null || !isProgramClass(name)) {
            "Duplicate class entry '$name'; exact instrumentation provenance is unavailable"
        }
    }

    private sealed interface EntrySource {
        fun open(): java.io.InputStream
        class Disk(private val file: File) : EntrySource {
            override fun open(): java.io.InputStream = file.inputStream()
        }
        class Archive(private val archive: ZipFile, private val entry: ZipEntry) : EntrySource {
            override fun open(): java.io.InputStream = archive.getInputStream(entry)
        }
    }
}

/** The sample app is a valid PROJECT; these library implementations are Jank Hunter itself. */
internal fun isJankHunterSupportClass(className: String): Boolean =
    InstrumentationPackages.isImplementationClass(className)

private class CausalHierarchyResolver(private val index: LifecycleClassIndex) {
    private val frameTypes = LifecycleFrameTypes(index)
    private val ancestry = HashMap<String, Set<String>>()

    fun resolve(name: String): Set<String> {
        val normalized = name.replace('.', '/')
        ancestry[normalized]?.let { return it }
        val resolved = linkedSetOf<String>()
        val pending = ArrayDeque<String>()
        pending.add(normalized)
        while (pending.isNotEmpty()) {
            val current = pending.removeFirst()
            if (!resolved.add(current)) continue
            val header = index.header(current) ?: continue
            header.superName?.let(pending::addLast)
            header.interfaces.forEach(pending::addLast)
        }
        return resolved.also { ancestry[normalized] = it }
    }

    fun common(first: String, second: String): String = frameTypes.common(first, second)
}
