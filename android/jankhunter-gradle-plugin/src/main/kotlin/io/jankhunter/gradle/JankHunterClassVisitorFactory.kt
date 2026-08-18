package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.gradle.api.GradleException
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.FieldVisitor
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val params = parameters.get()
        val classData = classContext.currentClassData
        val hookConfig = HookConfig(
            autoInit = params.autoInit.getOrElse(false),
            embeddedSymbols = params.embeddedSymbols.getOrElse(true),
            methodCounters = params.methodCounters.getOrElse(false),
            methodFilterMode = params.methodFilterMode.getOrElse(JankHunterMethodFilterMode.ENABLED),
            okhttp = params.okhttp.getOrElse(false),
            webSockets = params.webSockets.getOrElse(false),
            okHttpHelperAvailable = params.okHttpHelperAvailable.getOrElse(false),
            handlers = params.handlers.getOrElse(false),
            executors = params.executors.getOrElse(false),
            coroutines = params.coroutines.getOrElse(false),
            flowInteractions = params.flowInteractions.getOrElse(false),
            logSpam = params.logSpam.getOrElse(false),
            classGraph = params.classGraph.getOrElse(false),
            runtimeCallGraph = params.runtimeCallGraph.getOrElse(false),
            composeTracing = params.composeTracing.getOrElse(true),
            roomTracing = params.roomTracing.getOrElse(true),
            workerTracing = params.workerTracing.getOrElse(true),
            classGraphDirectory = params.classGraphDirectory.getOrElse(""),
            instrumentationDiagnosticsDirectory = params.instrumentationDiagnosticsDirectory.getOrElse(""),
            ownerMapEntriesDirectory = params.ownerMapEntriesDirectory.getOrElse(""),
            lifecycleLeaks = params.lifecycleLeaks.getOrElse(false),
        )
        val runtimeHooksMatch = runtimeInstrumentationMatches(classData, params)
        val autoInitMatch = autoInitMatches(classData, params)
        if (params.asmProgressLog.getOrElse(false)) {
            val progressLabel = buildList {
                if (runtimeHooksMatch || autoInitMatch) add(hookConfig.progressLabel())
                if (dependencyInjectionAnalysisMatches(classData, params)) add("di")
            }.joinToString("+").ifEmpty { "none" }
            AsmProgressReporter.recordInstrumented(
                params.progressLabel.getOrElse("unknown"),
                classData.className,
                progressLabel,
            )
        }
        var visitor = nextClassVisitor
        if (runtimeHooksMatch || autoInitMatch) {
            val hierarchyResolver = ClassHierarchyResolver(classContext)
            visitor = JankHunterClassVisitor(
                visitor,
                classData.className,
                if (runtimeHooksMatch) hookConfig else hookConfig.autoInitOnly(),
                classHierarchy = hierarchyResolver.resolve(classData.className),
                resolveOwnerHierarchy = hierarchyResolver::resolve,
                markerOnlyWhenHookApplied = !runtimeHooksMatch,
                diagnosticsOnlyWhenHookApplied = !runtimeHooksMatch,
            )
        }
        if (dependencyInjectionAnalysisMatches(classData, params)) {
            visitor = DependencyInjectionClassVisitor(
                visitor,
                classData.className,
                params.dependencyInjectionCatalogDirectory.getOrElse(""),
                generated = DependencyInjectionClassMatcher.isGeneratedDiClass(classData),
                generatedFramework = DependencyInjectionClassMatcher.generatedFramework(classData),
            )
        }
        return visitor
    }

    override fun isInstrumentable(classData: ClassData): Boolean {
        val params = parameters.get()
        val matched = runtimeInstrumentationMatches(classData, params) ||
            autoInitMatches(classData, params) ||
            dependencyInjectionAnalysisMatches(classData, params)
        if (params.asmProgressLog.getOrElse(false)) {
            AsmProgressReporter.recordScanned(
                params.progressLabel.getOrElse("unknown"),
                classData.className,
                matched,
            )
        }
        return matched
    }

    private fun autoInitMatches(
        classData: ClassData,
        params: JankHunterInstrumentationParameters,
    ): Boolean {
        if (!params.autoInit.getOrElse(false)) return false
        if (InstrumentationMarker.isPresent(classData.classAnnotations)) return false
        if (classData.className.startsWith("io.jankhunter.runtime.")) return false
        return AndroidComponentAutoInit.matches(classData)
    }

    private fun runtimeInstrumentationMatches(
        classData: ClassData,
        params: JankHunterInstrumentationParameters,
    ): Boolean {
        val hooksEnabled = params.methodCounters.getOrElse(false) ||
            params.okhttp.getOrElse(false) ||
            params.webSockets.getOrElse(false) ||
            params.handlers.getOrElse(false) ||
            params.executors.getOrElse(false) ||
            params.coroutines.getOrElse(false) ||
            params.flowInteractions.getOrElse(false) ||
            params.lifecycleLeaks.getOrElse(false) ||
            params.logSpam.getOrElse(false) ||
            params.classGraph.getOrElse(false) ||
            params.runtimeCallGraph.getOrElse(false) ||
            params.composeTracing.getOrElse(true) ||
            params.roomTracing.getOrElse(true) ||
            params.workerTracing.getOrElse(true)
        if (!hooksEnabled) return false
        if (InstrumentationMarker.isPresent(classData.classAnnotations)) return false
        if (DependencyInjectionClassMatcher.isGeneratedDiClass(classData)) return false
        return InstrumentationMatcher(
            params.includePackages.getOrElse(emptySet()),
            params.excludePackages.getOrElse(emptySet()),
            params.includeWholeApplication.getOrElse(false),
        ).matches(classData.className)
    }

    private fun dependencyInjectionAnalysisMatches(
        classData: ClassData,
        params: JankHunterInstrumentationParameters,
    ): Boolean {
        if (!params.dependencyInjectionAnalysis.getOrElse(false)) return false
        return DependencyInjectionClassMatcher.shouldScan(
            classData,
            params.includePackages.getOrElse(emptySet()),
            params.includeWholeApplication.getOrElse(false),
        )
    }
}

internal object InstrumentationMarker {
    const val DESCRIPTOR = "Lio/jankhunter/runtime/JankHunterInstrumented;"
    private const val CLASS_NAME = "io.jankhunter.runtime.JankHunterInstrumented"

    fun isPresent(annotations: Iterable<String>): Boolean {
        return hasInstrumentationMarker(annotations, DESCRIPTOR, CLASS_NAME)
    }
}

internal object LifecycleInstrumentationMarker {
    const val DESCRIPTOR = "Lio/jankhunter/runtime/JankHunterLifecycleInstrumented;"
    private const val CLASS_NAME = "io.jankhunter.runtime.JankHunterLifecycleInstrumented"

    fun isPresent(annotations: Iterable<String>): Boolean {
        return hasInstrumentationMarker(annotations, DESCRIPTOR, CLASS_NAME)
    }
}

private fun hasInstrumentationMarker(
    annotations: Iterable<String>,
    descriptor: String,
    className: String,
): Boolean {
    return annotations.any { annotation ->
        annotation == descriptor || annotation.replace('/', '.').removePrefix("L").removeSuffix(";") == className
    }
}

private class ClassHierarchyResolver(
    private val classContext: ClassContext,
) {
    private val cache = mutableMapOf<String, Set<String>>()

    fun resolve(className: String): Set<String> {
        val root = className.toInternalClassName()
        return cache.getOrPut(root) {
            val resolved = linkedSetOf(root)
            val pending = ArrayDeque<String>()
            pending.add(root)
            while (pending.isNotEmpty()) {
                val candidate = pending.removeFirst()
                val data = runCatching {
                    classContext.loadClassData(candidate.replace('/', '.'))
                }.getOrNull() ?: continue
                (data.superClasses + data.interfaces).forEach { parent ->
                    val normalized = parent.toInternalClassName()
                    if (resolved.add(normalized)) pending.add(normalized)
                }
            }
            resolved
        }
    }

    private fun String.toInternalClassName(): String = replace('.', '/')
}

internal data class HookConfig(
    val autoInit: Boolean = false,
    val embeddedSymbols: Boolean = true,
    val methodCounters: Boolean,
    val methodFilterMode: JankHunterMethodFilterMode = JankHunterMethodFilterMode.ENABLED,
    val okhttp: Boolean,
    val webSockets: Boolean,
    val okHttpHelperAvailable: Boolean = true,
    val handlers: Boolean,
    val executors: Boolean,
    val coroutines: Boolean,
    val flowInteractions: Boolean,
    val logSpam: Boolean,
    val classGraph: Boolean,
    val runtimeCallGraph: Boolean,
    val classGraphDirectory: String,
    val instrumentationDiagnosticsDirectory: String,
    val ownerMapEntriesDirectory: String,
    val lifecycleLeaks: Boolean = false,
    val composeTracing: Boolean = true,
    val roomTracing: Boolean = true,
    val workerTracing: Boolean = true,
) {
    fun progressLabel(): String {
        return buildList {
            if (autoInit) add("autoinit")
            if (methodCounters) add("methods")
            if (okhttp) add("okhttp")
            if (webSockets) add("websocket")
            if (handlers) add("handler")
            if (executors) add("executor")
            if (coroutines) add("coroutine")
            if (flowInteractions) add("flow")
            if (lifecycleLeaks) add("lifecycle")
            if (logSpam) add("logspam")
            if (classGraph) add("graph")
            if (runtimeCallGraph) add("runtimegraph")
            if (composeTracing) add("compose")
            if (roomTracing) add("room")
            if (workerTracing) add("worker")
        }.joinToString("+").ifEmpty { "none" }
    }

    fun autoInitOnly(): HookConfig = copy(
        methodCounters = false,
        okhttp = false,
        webSockets = false,
        handlers = false,
        executors = false,
        coroutines = false,
        flowInteractions = false,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        lifecycleLeaks = false,
        composeTracing = false,
        roomTracing = false,
        workerTracing = false,
    )
}

private object AndroidComponentAutoInit {
    private val componentBases = setOf(
        "android.app.Application",
        "android.app.Activity",
        "android.app.Service",
        "android.content.ContentProvider",
        "android.content.BroadcastReceiver",
    )

    fun matches(classData: ClassData): Boolean {
        return classData.superClasses.any { it.replace('/', '.') in componentBases }
    }
}

private enum class AutoInitComponent(
    val methodName: String,
    val methodDescriptor: String,
    val syntheticAccess: Int?,
) {
    APPLICATION("onCreate", "()V", Opcodes.ACC_PUBLIC),
    ACTIVITY("onCreate", "(Landroid/os/Bundle;)V", Opcodes.ACC_PROTECTED),
    SERVICE("onCreate", "()V", Opcodes.ACC_PUBLIC),
    CONTENT_PROVIDER("onCreate", "()Z", null),
    BROADCAST_RECEIVER(
        "onReceive",
        "(Landroid/content/Context;Landroid/content/Intent;)V",
        null,
    ),
    ;

    fun matches(name: String, descriptor: String): Boolean {
        return name == methodName && descriptor == methodDescriptor
    }

    companion object {
        fun fromHierarchy(hierarchy: Set<String>): AutoInitComponent? {
            return when {
                "android/app/Application" in hierarchy -> APPLICATION
                "android/app/Activity" in hierarchy -> ACTIVITY
                "android/app/Service" in hierarchy -> SERVICE
                "android/content/ContentProvider" in hierarchy -> CONTENT_PROVIDER
                "android/content/BroadcastReceiver" in hierarchy -> BROADCAST_RECEIVER
                else -> null
            }
        }
    }
}

internal class JankHunterClassVisitor(
    next: ClassVisitor,
    private val className: String,
    private val config: HookConfig,
    classHierarchy: Set<String> = setOf(className),
    private val resolveOwnerHierarchy: (String) -> Set<String> = { setOf(it) },
    private val instrumentationMarkerDescriptor: String = InstrumentationMarker.DESCRIPTOR,
    private val markerOnlyWhenHookApplied: Boolean = false,
    private val diagnosticsOnlyWhenHookApplied: Boolean = false,
) : ClassVisitor(Opcodes.ASM9, next) {
    private val edges = linkedMapOf<ClassGraphEdgeKey, Int>()
    private val ownerMapEntries = mutableListOf<OwnerMapEntry>()
    private val classAnnotations = JankAnnotationMetadata.Builder()
    private val diagnostics = InstrumentationDiagnosticsClassBuilder(className)
    private val classHierarchy = classHierarchy.mapTo(linkedSetOf()) { it.replace('.', '/') }
    private val autoInitComponent = if (config.autoInit) {
        AutoInitComponent.fromHierarchy(this.classHierarchy)
    } else {
        null
    }
    private var kotlinGeneratedMethods = KotlinGeneratedMethodIndex.EMPTY
    private var superName: String? = null
    private var classAccess: Int = 0
    private var autoInitMethodPresent = false
    private var alreadyInstrumented = false
    private var classHookApplied = false
    private var roomDatabaseFieldPresent = false

    override fun visit(
        version: Int,
        access: Int,
        name: String?,
        signature: String?,
        superName: String?,
        interfaces: Array<out String>?,
    ) {
        this.superName = superName
        this.classAccess = access
        name?.let { classHierarchy.add(it.replace('.', '/')) }
        superName?.let { classHierarchy.add(it.replace('.', '/')) }
        interfaces.orEmpty().forEach { classHierarchy.add(it.replace('.', '/')) }
        super.visit(version, access, name, signature, superName, interfaces)
    }

    override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
        val delegate = super.visitAnnotation(descriptor, visible)
        if (descriptor == instrumentationMarkerDescriptor) {
            alreadyInstrumented = true
            return delegate
        }
        if (
            descriptor == KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR &&
            config.methodFilterMode != JankHunterMethodFilterMode.DISABLED
        ) {
            return KotlinGeneratedMethodIndex.collectingVisitor(delegate) { kotlinGeneratedMethods = it }
        }
        return JankAnnotationParser.visitorFor(descriptor, delegate, classAnnotations)
    }

    override fun visitField(
        access: Int,
        name: String?,
        descriptor: String,
        signature: String?,
        value: Any?,
    ): FieldVisitor? {
        if (descriptor == ROOM_DATABASE_DESCRIPTOR) roomDatabaseFieldPresent = true
        return super.visitField(access, name, descriptor, signature, value)
    }

    override fun visitMethod(
        access: Int,
        name: String,
        descriptor: String,
        signature: String?,
        exceptions: Array<out String>?,
    ): MethodVisitor {
        val next = super.visitMethod(access, name, descriptor, signature, exceptions)
        val autoInitEntryPoint = autoInitComponent?.matches(name, descriptor) == true
        if (autoInitEntryPoint) autoInitMethodPresent = true
        if (alreadyInstrumented) {
            diagnostics.recordSkippedMethod("already_instrumented")
            return next
        }
        if (name == "<clinit>") {
            diagnostics.recordSkippedMethod("class_initializer")
            return next
        }
        if (access and Opcodes.ACC_ABSTRACT != 0) {
            diagnostics.recordSkippedMethod("abstract")
            return next
        }
        if (access and Opcodes.ACC_NATIVE != 0) {
            diagnostics.recordSkippedMethod("native")
            return next
        }
        return JankHunterMethodVisitor(
            next,
            access,
            name,
            descriptor,
            className,
            classAccess,
            kotlinGeneratedMethods.origin(name, descriptor),
            config,
            classAnnotations.snapshot(),
            name == "<init>",
            superName,
            classHierarchy,
            resolveOwnerHierarchy,
            diagnostics,
            autoInitComponent = autoInitComponent.takeIf { autoInitEntryPoint },
            recordClassHookApplied = { classHookApplied = true },
            recordOwnerMapEntry = ownerMapEntries::add,
            recordStaticEdge = { calleeOwner, calleeName ->
                recordStaticEdge(name, descriptor, calleeOwner, calleeName)
            },
            roomDaoMethod = isRoomDaoBoundary(access, name),
        )
    }

    private fun isRoomDaoBoundary(access: Int, name: String): Boolean {
        val excludedFlags = Opcodes.ACC_STATIC or Opcodes.ACC_SYNTHETIC or Opcodes.ACC_BRIDGE
        return config.roomTracing &&
            roomDatabaseFieldPresent &&
            access and Opcodes.ACC_PUBLIC != 0 &&
            access and excludedFlags == 0 &&
            name != "<init>" &&
            name != "getRequiredConverters"
    }

    override fun visitEnd() {
        emitSyntheticAutoInitMethodIfNeeded()
        if (!alreadyInstrumented && (!markerOnlyWhenHookApplied || classHookApplied)) {
            super.visitAnnotation(instrumentationMarkerDescriptor, false)?.visitEnd()
        }
        if (config.classGraph) {
            ClassGraphWriter.write(config.classGraphDirectory, className, edges)
        }
        if (ownerMapEntries.isNotEmpty()) {
            OwnerMapWriter.writeEntries(config.ownerMapEntriesDirectory, className, ownerMapEntries)
        }
        if (!diagnosticsOnlyWhenHookApplied || classHookApplied) {
            InstrumentationDiagnosticsWriter.write(
                config.instrumentationDiagnosticsDirectory,
                diagnostics.finish(),
            )
        }
        super.visitEnd()
    }

    private fun emitSyntheticAutoInitMethodIfNeeded() {
        val component = autoInitComponent ?: return
        val access = component.syntheticAccess ?: return
        val parent = superName ?: return
        if (alreadyInstrumented || autoInitMethodPresent) return
        if (classAccess and (Opcodes.ACC_ABSTRACT or Opcodes.ACC_INTERFACE) != 0) return

        super.visitMethod(access, component.methodName, component.methodDescriptor, null, null).apply {
            visitCode()
            visitVarInsn(Opcodes.ALOAD, 0)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_RUNTIME,
                "autoInit",
                "(Landroid/content/Context;)V",
                false,
            )
            visitVarInsn(Opcodes.ALOAD, 0)
            if (component == AutoInitComponent.ACTIVITY) {
                visitVarInsn(Opcodes.ALOAD, 1)
            }
            visitMethodInsn(
                Opcodes.INVOKESPECIAL,
                parent,
                component.methodName,
                component.methodDescriptor,
                false,
            )
            visitInsn(Opcodes.RETURN)
            visitMaxs(if (component == AutoInitComponent.ACTIVITY) 2 else 1, if (component == AutoInitComponent.ACTIVITY) 2 else 1)
            visitEnd()
        }
        classHookApplied = true
    }

    private companion object {
        private const val JANK_HUNTER_RUNTIME = "io/jankhunter/runtime/JankHunter"
        private const val ROOM_DATABASE_DESCRIPTOR = "Landroidx/room/RoomDatabase;"
    }

    private fun recordStaticEdge(
        callerName: String,
        callerDescriptor: String,
        calleeOwner: String,
        calleeName: String,
    ) {
        if (!config.classGraph) return
        if (!ClassGraphWriter.isApplicationLike(calleeOwner)) return
        val key = ClassGraphEdgeKey(
            caller = "$callerName$callerDescriptor",
            calleeClass = calleeOwner.replace('/', '.'),
            calleeMethod = calleeName,
        )
        edges[key] = (edges[key] ?: 0) + 1
    }
}

private class JankHunterMethodVisitor(
    next: MethodVisitor,
    private val accessFlags: Int,
    private val methodName: String,
    private val methodDescriptor: String,
    private val className: String,
    private val classAccessFlags: Int,
    private val kotlinMethodOrigin: KotlinMethodOrigin,
    private val config: HookConfig,
    private val classAnnotations: JankAnnotationMetadata,
    private val constructor: Boolean,
    private val superName: String?,
    private val classHierarchy: Set<String>,
    private val resolveOwnerHierarchy: (String) -> Set<String>,
    private val diagnostics: InstrumentationDiagnosticsClassBuilder,
    private val autoInitComponent: AutoInitComponent?,
    private val recordClassHookApplied: () -> Unit,
    private val recordOwnerMapEntry: (OwnerMapEntry) -> Unit,
    private val recordStaticEdge: (String, String) -> Unit,
    private val roomDaoMethod: Boolean = false,
) : AdviceAdapter(Opcodes.ASM9, next, accessFlags, methodName, methodDescriptor) {
    private val methodId = OwnerIds.methodId(className, methodName, methodDescriptor)
    private val generatedOwnerLabel = OwnerIds.readableOwner(className, methodName)
    private val methodDiagnosticName = "$methodName$methodDescriptor"
    private val methodAnnotations = JankAnnotationMetadata.Builder()
    private val ownerLabel: String
        get() = methodAnnotations.owner?.takeIf { it.isNotBlank() } ?: classAnnotations.owner ?: generatedOwnerLabel
    private val annotationScreen: String?
        get() = methodAnnotations.screen?.takeIf { it.isNotBlank() }
            ?: classAnnotations.screen.takeIf { !constructor || constructorHasDirectAnnotationContext }
    private val annotationFlow: String?
        get() = methodAnnotations.flow?.takeIf { it.isNotBlank() }
            ?: classAnnotations.flow.takeIf { !constructor || constructorHasDirectAnnotationContext }
    private val annotationTrace: String?
        get() {
            methodAnnotations.trace?.takeIf { it.isNotBlank() }?.let { return it }
            if (methodAnnotations.tracePresent) return methodName
            if (constructor && !constructorHasDirectAnnotationContext) return null
            classAnnotations.trace?.takeIf { it.isNotBlank() }?.let { return it }
            if (classAnnotations.tracePresent) return methodName
            return null
        }
    private val constructorHasDirectAnnotationContext: Boolean
        get() = methodAnnotations.screen?.takeIf { it.isNotBlank() } != null ||
            methodAnnotations.flow?.takeIf { it.isNotBlank() } != null ||
            methodAnnotations.trace?.takeIf { it.isNotBlank() } != null ||
            methodAnnotations.tracePresent ||
            methodAnnotations.owner?.takeIf { it.isNotBlank() } != null
    private val hasAnnotationContext: Boolean
        get() = annotationScreen != null ||
            annotationFlow != null ||
            annotationTrace != null ||
            methodAnnotations.owner?.takeIf { it.isNotBlank() } != null ||
            classAnnotations.owner != null && (!constructor || constructorHasDirectAnnotationContext)
    private val hookEmitter = HookBytecodeEmitter(
        visitor = this,
        ownerLabel = { ownerLabel },
        emitOriginal = ::emitOriginalInvocation,
        emitTryCatchBlock = ::emitPostSuperTryCatchBlock,
    )
    private var runtimeCallStartLocal = -1
    private var annotationScopeLocal = -1
    private var semanticStartLocal = -1
    private var semanticOutcomeLocal = -1
    private val methodTryStart = Label()
    private val methodTryEnd = Label()
    private val methodExceptionHandler = Label()
    private var currentLine: Int? = null
    private var hookApplied = false
    private var constructorBodyEntered = false
    private val methodFilterDecision = MethodFilterClassifier.classify(
        config.methodFilterMode,
        classAccessFlags,
        accessFlags,
        className,
        methodName,
        methodDescriptor,
        kotlinMethodOrigin,
    )

    override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
        val delegate = super.visitAnnotation(descriptor, visible)
        return JankAnnotationParser.visitorFor(descriptor, delegate, methodAnnotations)
    }

    override fun visitLineNumber(line: Int, start: Label) {
        currentLine = line
        super.visitLineNumber(line, start)
    }

    override fun onMethodEnter() {
        if (constructor) {
            // The JVM forbids using an uninitialized `this`. AdviceAdapter calls this only after
            // the first this()/super() invocation, which is the earliest safe constructor boundary.
            constructorBodyEntered = true
        }
        autoInitComponent?.let {
            emitAutoInit(it)
            recordClassHookApplied()
            hookApplied = true
        }
        if (!shouldInstrumentMethod()) return
        if (shouldWatchLifecycleOnEnter()) {
            emitLifecycleWatch()
        }
        if (hasAnnotationContext) {
            emitEnterAnnotatedContext()
            hookApplied = true
        }
        if (config.methodCounters && methodBoundaryHooksEnabled()) {
            visitLdcInsn(methodId)
            if (config.embeddedSymbols) visitLdcInsn(generatedOwnerLabel)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "recordMethodCall",
                if (config.embeddedSymbols) "(JLjava/lang/String;)V" else "(J)V",
                false,
            )
            hookApplied = true
        }
        if (config.runtimeCallGraph && methodBoundaryHooksEnabled()) {
            visitLdcInsn(methodId)
            if (config.embeddedSymbols) visitLdcInsn(generatedOwnerLabel)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "enterMethod",
                if (config.embeddedSymbols) "(JLjava/lang/String;)J" else "(J)J",
                false,
            )
            runtimeCallStartLocal = newLocal(Type.LONG_TYPE)
            storeLocal(runtimeCallStartLocal)
            hookApplied = true
        }
        semanticKind()?.let { kind ->
            visitLdcInsn(kind)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "enterSemantic",
                "(I)J",
                false,
            )
            semanticStartLocal = newLocal(Type.LONG_TYPE)
            storeLocal(semanticStartLocal)
            if (kind == SEMANTIC_WORKER) {
                semanticOutcomeLocal = newLocal(Type.INT_TYPE)
                visitLdcInsn(SEMANTIC_OUTCOME_UNKNOWN)
                storeLocal(semanticOutcomeLocal)
            }
            hookApplied = true
        }
        if (requiresCatchAllExit()) {
            visitLabel(methodTryStart)
        }
    }

    override fun onMethodExit(opcode: Int) {
        if (!shouldInstrumentMethod()) return
        if (shouldWatchLifecycleOnExit() && opcode != Opcodes.ATHROW) {
            emitLifecycleWatch()
        }
        if (config.runtimeCallGraph && runtimeCallStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitRuntimeCallExit()
        }
        if (semanticStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            if (semanticOutcomeLocal >= 0 && opcode == Opcodes.ARETURN) {
                dup()
                visitMethodInsn(
                    Opcodes.INVOKESTATIC,
                    JANK_HUNTER_HOOKS,
                    "classifyWorkerOutcome",
                    "(Ljava/lang/Object;)I",
                    false,
                )
                storeLocal(semanticOutcomeLocal)
            }
            emitSemanticExit(outcome = SEMANTIC_OUTCOME_SUCCESS, outcomeLocal = semanticOutcomeLocal)
        }
        if (annotationScopeLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitExitAnnotatedContext()
        }
    }

    private fun emitRuntimeCallExit() {
        loadLocal(runtimeCallStartLocal)
        if (constructor) {
            // AdviceAdapter conservatively treats every constructor exception handler as a
            // pre-super branch. Real Kotlin constructors commonly have post-super handlers, so
            // returning through one can reset its simulated stack even though `this` is already
            // initialized. GeneratorAdapter emits loadLocal directly to `mv`; keep the rest of
            // this synthetic sequence on the same path to avoid consuming that empty simulation.
            mv.visitLdcInsn(methodId)
            mv.visitMethodInsn(Opcodes.INVOKESTATIC, JANK_HUNTER_HOOKS, "exitMethod", "(JJ)V", false)
        } else {
            visitLdcInsn(methodId)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "exitMethod",
                "(JJ)V",
                false,
            )
        }
    }

    private fun emitSemanticExit(outcome: Int, outcomeLocal: Int = -1) {
        val kind = semanticKind() ?: return
        loadLocal(semanticStartLocal)
        visitLdcInsn(kind)
        visitLdcInsn(methodId)
        pushNullableString(if (config.embeddedSymbols) generatedOwnerLabel else null)
        if (outcomeLocal >= 0) {
            loadLocal(outcomeLocal)
        } else {
            visitLdcInsn(outcome)
        }
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "exitSemantic",
            "(JIJLjava/lang/String;I)V",
            false,
        )
    }

    private fun semanticKind(): Int? {
        if (constructor) return null
        if (accessFlags and (Opcodes.ACC_SYNTHETIC or Opcodes.ACC_BRIDGE) != 0) return null
        if (config.composeTracing && methodAnnotations.composable) return SEMANTIC_COMPOSE_COMPOSITION
        if (roomDaoMethod) return SEMANTIC_ROOM_DAO
        if (config.workerTracing && isSynchronousWorkerMethod()) return SEMANTIC_WORKER
        return null
    }

    private fun isSynchronousWorkerMethod(): Boolean {
        return methodName == "doWork" && methodDescriptor == WORKER_DO_WORK_DESCRIPTOR &&
            ANDROIDX_WORKER in classHierarchy
    }

    override fun visitMethodInsn(
        opcodeAndSource: Int,
        owner: String,
        name: String,
        descriptor: String,
        isInterface: Boolean,
    ) {
        if (!shouldInstrumentMethod()) {
            super.visitMethodInsn(opcodeAndSource, owner, name, descriptor, isInterface)
            return
        }
        recordStaticEdge(owner, name)
        val call = MethodCall(
            owner = owner,
            name = name,
            descriptor = descriptor,
            caller = CallerMethod(className, methodName, methodDescriptor),
            line = currentLine,
            ownerHierarchy = resolveOwnerHierarchy(owner),
        )
        val decision = HookIntentResolver.resolve(call, config)
        if (
            decision is HookDecision.Matched &&
            decision.intent.requiresOkHttpHelper() &&
            !config.okHttpHelperAvailable
        ) {
            throw missingOkHttpHelper(call)
        }
        val invocation = MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface)
        if (decision is HookDecision.Matched && emitHook(decision.intent, invocation)) {
            diagnostics.recordHook(decision, methodDiagnosticName, call.line)
            return
        }
        if (decision is HookDecision.Matched) {
            diagnostics.recordHook(decision, methodDiagnosticName, call.line)
        } else {
            diagnostics.recordDecision(
                if (decision is HookDecision.NotMatched) {
                    HookNearMissDiagnostics.resolve(call, config) ?: decision
                } else {
                    decision
                },
                methodDiagnosticName,
                call.line,
            )
        }
        super.visitMethodInsn(opcodeAndSource, owner, name, descriptor, isInterface)
    }

    private fun HookIntent.requiresOkHttpHelper(): Boolean {
        return when (this) {
            HookIntent.WrapOkHttpEventListenerFactory,
            HookIntent.InstallOkHttpEventListenerFactory,
            HookIntent.WrapWebSocketListener,
            -> true
            is HookIntent.HandlerRunnable,
            is HookIntent.HandlerRemoveCallbacks,
            HookIntent.HandlerRemoveCallbacksAndMessages,
            HookIntent.HandlerHasCallbacks,
            HookIntent.HandlerMessageSend,
            is HookIntent.ExecutorRunnable,
            is HookIntent.ExecutorCallable,
            is HookIntent.CoroutineBlock,
            HookIntent.WrapClickListener,
            is HookIntent.LogSpam,
            -> false
        }
    }

    private fun missingOkHttpHelper(call: MethodCall): GradleException {
        val coordinates = JankHunterDependencyCoordinates.load()
        val dependency = "${coordinates.group}:jankhunter-okhttp3:${coordinates.version}"
        val sourceLocation = buildString {
            append(className.replace('/', '.'))
            append('#')
            append(methodName)
            append(methodDescriptor)
            append(" at line ")
            append(call.line ?: "unknown (no LineNumberTable)")
        }
        return GradleException(
            "Jank Hunter matched OkHttp/WebSocket call " +
                "${call.owner.replace('/', '.')}.${call.name}${call.descriptor} in $sourceLocation, " +
                "but runtime helper '$dependency' is not declared for this variant. " +
                "Add implementation(\"$dependency\") (or the matching variantImplementation dependency) " +
                "before enabling jankHunter.instrument.okhttp/webSockets. Instrumentation stopped before " +
                "emitting bytecode that could crash the host app.",
        )
    }

    private fun emitHook(intent: HookIntent, invocation: MethodInvocation): Boolean {
        val command = BytecodeCommandFactory.commandFor(intent)
        command.emit(hookEmitter, invocation)
        hookApplied = true
        return command.replacesOriginalCall
    }

    private fun emitOriginalInvocation(invocation: MethodInvocation) {
        super.visitMethodInsn(
            invocation.opcodeAndSource,
            invocation.owner,
            invocation.name,
            invocation.descriptor,
            invocation.isInterface,
        )
    }

    /**
     * Hook commands are admitted in constructors only after AdviceAdapter observed this()/super().
     * Bypass its conservative constructor handler bookkeeping for generated post-super regions:
     * otherwise visiting the handler resets its internal state to "before super" and corrupts the
     * simulated operand stack even though every protected instruction is after initialization.
     */
    private fun emitPostSuperTryCatchBlock(start: Label, end: Label, handler: Label, type: String?) {
        mv.visitTryCatchBlock(start, end, handler, type)
    }

    override fun visitMaxs(maxStack: Int, maxLocals: Int) {
        if (shouldInstrumentMethod() && requiresCatchAllExit()) {
            visitLabel(methodTryEnd)
            if (constructor) {
                emitPostSuperTryCatchBlock(methodTryStart, methodTryEnd, methodExceptionHandler, null)
            } else {
                visitTryCatchBlock(methodTryStart, methodTryEnd, methodExceptionHandler, null)
            }
            visitLabel(methodExceptionHandler)
            val throwableLocal = newLocal(Type.getType(Throwable::class.java))
            storeLocal(throwableLocal)
            if (config.runtimeCallGraph && runtimeCallStartLocal >= 0) {
                emitRuntimeCallExit()
            }
            if (semanticStartLocal >= 0) {
                emitSemanticExit(outcome = SEMANTIC_OUTCOME_FAILURE)
            }
            if (annotationScopeLocal >= 0) {
                emitExitAnnotatedContext()
            }
            loadLocal(throwableLocal)
            mv.visitInsn(Opcodes.ATHROW)
        }
        super.visitMaxs(maxStack + 6, maxLocals)
    }

    override fun visitEnd() {
        val ignored = instrumentationIgnored()
        if (!ignored && shouldInstrumentMethod() && hookApplied) {
            recordOwnerMapEntry(
                OwnerMapEntry(
                    id = methodId,
                    owner = OwnerIds.readableOwner(className, methodName),
                    className = className.replace('/', '.'),
                    methodName = methodName,
                    descriptor = methodDescriptor,
                ),
            )
        }
        diagnostics.recordMethod(
            ignored = ignored,
            annotation = if (!ignored) annotationDiagnosticKey() else null,
        )
        if (config.methodFilterMode != JankHunterMethodFilterMode.DISABLED) {
            diagnostics.recordMethodFilter(
                methodFilterDecision,
                excluded = config.methodFilterMode == JankHunterMethodFilterMode.ENABLED &&
                    methodFilterDecision.exclusionReason != null,
            )
        }
        super.visitEnd()
    }

    private companion object {
        private const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
        private const val JANK_HUNTER_RUNTIME = "io/jankhunter/runtime/JankHunter"
        private const val ANDROID_ACTIVITY = "android/app/Activity"
        private const val ANDROID_FRAGMENT = "android/app/Fragment"
        private const val ANDROID_SERVICE = "android/app/Service"
        private const val ANDROIDX_FRAGMENT = "androidx/fragment/app/Fragment"
        private const val ANDROIDX_VIEW_MODEL = "androidx/lifecycle/ViewModel"
        private const val ANDROIDX_WORKER = "androidx/work/Worker"
        private const val WORKER_DO_WORK_DESCRIPTOR = "()Landroidx/work/ListenableWorker${'$'}Result;"
        private const val SEMANTIC_COMPOSE_COMPOSITION = 1
        private const val SEMANTIC_ROOM_DAO = 5
        private const val SEMANTIC_WORKER = 6
        private const val SEMANTIC_OUTCOME_SUCCESS = 0
        private const val SEMANTIC_OUTCOME_FAILURE = 1
        private const val SEMANTIC_OUTCOME_UNKNOWN = 4
    }

    private fun emitAutoInit(component: AutoInitComponent) {
        when (component) {
            AutoInitComponent.APPLICATION,
            AutoInitComponent.ACTIVITY,
            AutoInitComponent.SERVICE,
            -> loadThis()
            AutoInitComponent.CONTENT_PROVIDER -> {
                loadThis()
                visitMethodInsn(
                    Opcodes.INVOKEVIRTUAL,
                    "android/content/ContentProvider",
                    "getContext",
                    "()Landroid/content/Context;",
                    false,
                )
            }
            AutoInitComponent.BROADCAST_RECEIVER -> loadArg(0)
        }
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_RUNTIME,
            "autoInit",
            "(Landroid/content/Context;)V",
            false,
        )
    }

    private fun emitEnterAnnotatedContext() {
        pushNullableString(annotationScreen)
        pushNullableString(ownerLabel)
        pushNullableString(annotationFlow)
        pushNullableString(annotationTrace)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "enterAnnotatedContext",
            "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)Ljava/lang/Object;",
            false,
        )
        annotationScopeLocal = newLocal(Type.getType("Ljava/lang/Object;"))
        storeLocal(annotationScopeLocal)
    }

    private fun emitExitAnnotatedContext() {
        loadLocal(annotationScopeLocal)
        if (constructor) {
            mv.visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "exitAnnotatedContext",
                "(Ljava/lang/Object;)V",
                false,
            )
        } else {
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "exitAnnotatedContext",
                "(Ljava/lang/Object;)V",
                false,
            )
        }
    }

    private fun pushNullableString(value: String?) {
        if (value == null) {
            visitInsn(Opcodes.ACONST_NULL)
        } else {
            visitLdcInsn(value)
        }
    }

    private fun requiresCatchAllExit(): Boolean {
        return (config.runtimeCallGraph && runtimeCallStartLocal >= 0) || annotationScopeLocal >= 0 ||
            semanticStartLocal >= 0
    }

    private fun instrumentationIgnored(): Boolean {
        return classAnnotations.ignored || methodAnnotations.ignored
    }

    private fun shouldInstrumentMethod(): Boolean {
        return !instrumentationIgnored() && (!constructor || constructorBodyEntered)
    }

    private fun methodBoundaryHooksEnabled(): Boolean {
        return config.methodFilterMode != JankHunterMethodFilterMode.ENABLED ||
            methodFilterDecision.exclusionReason == null
    }

    private fun shouldWatchLifecycleOnEnter(): Boolean {
        if (!config.lifecycleLeaks || constructor || methodIsStatic()) return false
        return methodName == "onDestroyView" &&
            methodDescriptor == "()V" &&
            isLifecycleType(ANDROIDX_FRAGMENT, ANDROID_FRAGMENT)
    }

    private fun shouldWatchLifecycleOnExit(): Boolean {
        if (!config.lifecycleLeaks || constructor || methodIsStatic()) return false
        if (!lifecycleMethodDescriptorSupported()) return false
        if (methodName == "onDestroyView") return false
        return when (methodName) {
            "onDestroy" -> isLifecycleType(ANDROID_ACTIVITY, ANDROIDX_FRAGMENT, ANDROID_FRAGMENT, ANDROID_SERVICE)
            "onCleared" -> isLifecycleType(ANDROIDX_VIEW_MODEL)
            else -> false
        }
    }

    private fun lifecycleMethodDescriptorSupported(): Boolean {
        return methodDescriptor == "()V"
    }

    private fun methodIsStatic(): Boolean {
        return accessFlags and Opcodes.ACC_STATIC != 0
    }

    private fun isLifecycleType(vararg baseTypes: String): Boolean {
        return baseTypes.any(classHierarchy::contains)
    }

    private fun emitLifecycleWatch() {
        loadThis()
        visitLdcInsn(methodName)
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "watchLifecycleObject",
            "(Ljava/lang/Object;Ljava/lang/String;Ljava/lang/String;)V",
            false,
        )
        diagnostics.recordLifecycleHook(methodName, methodDescriptor, superName)
        recordClassHookApplied()
        hookApplied = true
    }

    private fun annotationDiagnosticKey(): AnnotationDiagnosticKey? {
        if (!hasAnnotationContext) return null
        return AnnotationDiagnosticKey(
            owner = ownerLabel,
            screen = annotationScreen,
            flow = annotationFlow,
            trace = annotationTrace,
        )
    }

}

internal data class ClassGraphEdgeKey(
    val caller: String,
    val calleeClass: String,
    val calleeMethod: String,
)

internal object ClassGraphWriter {
    fun write(directoryPath: String, className: String, edges: Map<ClassGraphEdgeKey, Int>) {
        if (directoryPath.isBlank() || edges.isEmpty()) return
        InstrumentationArtifactFiles.writeClassShard(directoryPath, className, record(className.replace('/', '.'), edges))
    }

    fun isApplicationLike(owner: String): Boolean {
        return !InstrumentationPackages.isBuiltinExcluded(owner)
    }

    private fun record(className: String, edges: Map<ClassGraphEdgeKey, Int>): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.CLASS_GRAPH_FORMAT)
            append(",\"class\":\"")
            append(escapeJsonString(className))
            append("\",\"edges\":[")
            edges.entries.forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append("{\"caller\":\"")
                append(escapeJsonString(entry.key.caller))
                append("\",\"calleeClass\":\"")
                append(escapeJsonString(entry.key.calleeClass))
                append("\",\"calleeMethod\":\"")
                append(escapeJsonString(entry.key.calleeMethod))
                append("\",\"count\":")
                append(entry.value)
                append('}')
            }
            append("]}\n")
        }
    }

}

internal enum class HandlerRunnableKind {
    SINGLE_RUNNABLE,
    FRONT_RUNNABLE,
    RUNNABLE_LONG_DELAY,
    RUNNABLE_LONG_TIME,
    RUNNABLE_OBJECT_LONG_DELAY,
    RUNNABLE_OBJECT_LONG_TIME,
}

internal enum class HandlerRemoveCallbacksKind {
    RUNNABLE,
    RUNNABLE_OBJECT,
}

internal enum class ExecutorRunnableKind {
    SINGLE_RUNNABLE,
    RUNNABLE_OBJECT,
    RUNNABLE_LONG_OBJECT,
    RUNNABLE_LONG_LONG_OBJECT,
}

internal enum class ExecutorCallableKind {
    SINGLE_CALLABLE,
    CALLABLE_LONG_OBJECT,
}

internal enum class CoroutineBlockKind {
    TOP_FUNCTION2,
    FUNCTION2_BEFORE_CONTINUATION,
    FUNCTION2_BEFORE_INT_OBJECT,
}

internal object OwnerIds {
    const val STABLE_ID_ALGORITHM =
        "fnv1a64-utf8(internal-class,NUL,method,NUL,descriptor);" +
            "offset=0xcbf29ce484222325;prime=0x100000001b3;v=1"
    const val STABLE_ID_ENCODING = "stable:0x%016x"

    fun readableOwner(className: String, methodName: String): String {
        return "${className.replace('/', '.')}.$methodName"
    }

    fun methodId(className: String, methodName: String, descriptor: String): Long {
        val internalClassName = className.replace('.', '/')
        return fnv1a64("$internalClassName\u0000$methodName\u0000$descriptor").toLong()
    }

    fun canonical(methodId: Long): String {
        return "stable:0x${methodId.toULong().toString(16).padStart(16, '0')}"
    }

    private fun fnv1a64(value: String): ULong {
        var hash = 0xcbf29ce484222325UL
        for (byte in value.encodeToByteArray()) {
            hash = hash xor byte.toUByte().toULong()
            hash *= 0x100000001b3UL
        }
        return hash
    }
}
