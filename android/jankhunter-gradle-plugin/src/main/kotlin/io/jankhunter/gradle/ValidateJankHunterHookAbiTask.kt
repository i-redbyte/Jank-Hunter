package io.jankhunter.gradle

import java.io.File
import java.util.zip.ZipFile
import org.gradle.api.DefaultTask
import org.gradle.api.file.ConfigurableFileCollection
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.provider.Property
import org.gradle.api.provider.SetProperty
import org.gradle.api.tasks.CacheableTask
import org.gradle.api.tasks.Classpath
import org.gradle.api.tasks.Input
import org.gradle.api.tasks.OutputFile
import org.gradle.api.tasks.TaskAction
import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.FieldVisitor
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes

/** SDK-only headers: neither application bodies nor the dependency classpath are scanned. */
@CacheableTask
abstract class ValidateJankHunterHookAbiTask : DefaultTask() {
    @get:Classpath abstract val compileSdk: ConfigurableFileCollection
    @get:Classpath abstract val runtimeSdk: ConfigurableFileCollection
    @get:Input abstract val requireSdk: Property<Boolean>
    @get:Input abstract val requiredOwners: SetProperty<String>
    @get:OutputFile abstract val outputFile: RegularFileProperty

    init {
        requireSdk.convention(false)
        requiredOwners.convention(emptySet())
    }

    @TaskAction fun validate() {
        HookAbiValidation.validate(compileSdk.files, runtimeSdk.files, requireSdk.get(), requiredOwners.get())
            .write(outputFile.get().asFile)
    }
}

/** Execution-only SDK header validation shared with AGP factories; no task-output providers. */
internal object HookAbiValidation {
    fun validate(compileFiles: Set<File>, runtimeFiles: Set<File>, requireSdk: Boolean, requiredOwners: Set<String>): HookAbiIndex {
        val owners = HookAbiRequirements.scanOwners(requiredOwners)
        val compile = headers(compileFiles, owners)
        val runtime = headers(runtimeFiles, owners)
        check(!requireSdk || "io/jankhunter/runtime/JankHunterHooks" in compile) {
            "Jank Hunter runtime SDK is missing from compile classpath"
        }
        // A library may expose compileOnly SDK references fulfilled by the final application.
        // Still validate emitted calls against the available compile headers in that case.
        val compileOnlyLibrary = !requireSdk && runtime.isEmpty()
        if (compile.isNotEmpty() && !compileOnlyLibrary) HookAbiIndex.requireRuntimeSurface(compile, runtime)
        if (owners.any { it == "io/jankhunter/runtime/JankHunterLifecycleAccessorV1" }) {
            LifecycleRuntimeAbi.validateSdk(compile)
        }
        return HookAbiIndex(
            if (compileOnlyLibrary) compile else runtime,
            deferred = compile.isEmpty() && !requireSdk,
            requiredOwners = requiredOwners,
        )
    }

    private fun headers(files: Set<File>, owners: Set<String>): Map<String, HookAbiClass> {
        if (owners.isEmpty()) return emptyMap()
        val result = LinkedHashMap<String, HookAbiClass>()
        files.sortedBy { it.absolutePath }.forEach { file ->
            if (file.isDirectory) {
                owners.forEach { owner ->
                    file.resolve("$owner.class").takeIf(File::isFile)?.let { add(result, owner, it.readBytes()) }
                }
            } else if (file.extension == "jar") {
                ZipFile(file).use { zip ->
                    owners.forEach { owner ->
                        zip.getEntry("$owner.class")?.let { entry ->
                            zip.getInputStream(entry).use { add(result, owner, it.readBytes()) }
                        }
                    }
                }
            }
        }
        return result
    }

    private fun add(result: MutableMap<String, HookAbiClass>, owner: String, bytes: ByteArray) {
        var access = 0
        var parent: String? = null
        var interfaces = emptyList<String>()
        val methods = LinkedHashMap<String, Int>()
        val fields = LinkedHashMap<String, Int>()
        ClassReader(bytes).accept(object : ClassVisitor(Opcodes.ASM9) {
            override fun visit(version: Int, flags: Int, name: String, signature: String?, superName: String?, implemented: Array<out String>?) {
                check(name == owner); access = flags; parent = superName; interfaces = implemented?.toList().orEmpty()
            }
            override fun visitMethod(flags: Int, name: String, descriptor: String, signature: String?, exceptions: Array<out String>?): MethodVisitor? {
                methods[name + descriptor] = flags; return null
            }
            override fun visitField(flags: Int, name: String, descriptor: String, signature: String?, value: Any?): FieldVisitor? {
                fields[name + descriptor] = flags; return null
            }
        }, ClassReader.SKIP_CODE or ClassReader.SKIP_DEBUG or ClassReader.SKIP_FRAMES)
        val header = HookAbiClass(access, parent, interfaces, methods, fields)
        val previous = result.put(owner, header)
        check(previous == null || previous == header) { "Conflicting Jank Hunter SDK definitions: $owner" }
    }
}
