package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.Attribute
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.FieldVisitor
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.commons.AdviceAdapter
import org.objectweb.asm.commons.CodeSizeEvaluator
import org.objectweb.asm.tree.MethodNode
import org.objectweb.asm.tree.MethodInsnNode
import org.objectweb.asm.tree.LineNumberNode
import org.objectweb.asm.tree.FrameNode

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val params = parameters.get()
        val classData = classContext.currentClassData
        val classOrigin = CausalCodeOrigin.UNKNOWN
        val selection = InstrumentationClassSelector.evaluate(params, classData, classOrigin)
        val hookConfig = InstrumentationHookConfigFactory.create(params)
        var visitor = HookAbiValidationSession.index(params).visitor(nextClassVisitor)
        if (selection.runtime || selection.autoInit || selection.networkBoundary || selection.databaseBoundary) {
            val hierarchyResolver = if (selection.runtime || selection.autoInit) {
                ClassHierarchyResolver(classContext)
            } else {
                null
            }
            visitor = JankHunterClassVisitor(
                visitor,
                classData.className,
                when {
                    selection.runtime -> hookConfig.copy(lifecycleLeaks = false)
                    selection.networkBoundary || selection.databaseBoundary -> hookConfig.boundaryOnly(
                        network = selection.networkBoundary,
                        database = selection.databaseBoundary,
                    )
                    else -> hookConfig.autoInitOnly()
                },
                classOrigin = classOrigin,
                classHierarchy = hierarchyResolver?.resolve(classData.className).orEmpty(),
                resolveOwnerHierarchy = hierarchyResolver
                    ?.let { resolver -> resolver::resolve }
                    ?: { emptySet() },
                hierarchyResolutionDiagnostics = hierarchyResolver
                    ?.let { resolver -> resolver::diagnostics }
                    ?: { emptyMap() },
                markerOnlyWhenHookApplied = !selection.runtime,
                diagnosticsOnlyWhenHookApplied = !selection.runtime,
            )
        }
        if (selection.dependencyInjection) {
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
        return InstrumentationClassSelector.evaluate(
            parameters.get(), classData, CausalCodeOrigin.UNKNOWN,
        ).any()
    }
}

internal class JankHunterClassVisitor(
    next: ClassVisitor,
    private val className: String,
    private val config: HookConfig,
    private val classOrigin: CausalCodeOrigin = CausalCodeOrigin.UNKNOWN,
    classHierarchy: Set<String> = setOf(className),
    private val resolveOwnerHierarchy: (String) -> Set<String> = { setOf(it) },
    private val hierarchyResolutionDiagnostics: () -> Map<ClassHierarchyResolutionFailure, Int> = { emptyMap() },
    private val instrumentationMarkerDescriptor: String = InstrumentationMarker.DESCRIPTOR,
    private val markerOnlyWhenHookApplied: Boolean = false,
    private val diagnosticsOnlyWhenHookApplied: Boolean = false,
    private val syntheticLifecycleCallbacks: Map<String, Int>? = null,
    private val existingLifecycleMethods: Set<String> = emptySet(),
    private val diagnosticsSink: ((InstrumentationDiagnosticsRecord) -> Unit)? = null,
) : ClassVisitor(Opcodes.ASM9, next) {
    private val edges = linkedMapOf<ClassGraphEdgeKey, Int>()
    private val lambdaCaptures = if (config.classGraph) LambdaCaptureClassBuilder(className) else null
    private val classAnnotations = JankAnnotationMetadata.Builder()
    private val diagnostics = InstrumentationDiagnosticsClassBuilder(
        className,
        when {
            instrumentationMarkerDescriptor == LifecycleInstrumentationMarker.DESCRIPTOR -> "lifecycle"
            config.metadataDecodePathway == MetadataDecodePathway.CAUSAL -> "causal"
            else -> "main"
        },
    )
    private val diagnosticsPass = when {
        instrumentationMarkerDescriptor == LifecycleInstrumentationMarker.DESCRIPTOR -> "lifecycle"
        config.metadataDecodePathway == MetadataDecodePathway.CAUSAL -> "causal"
        else -> "main"
    }
    private var previousDiagnostics: Attribute? = null
    private var previousArtifacts: MutableMap<String, Attribute>? = null

    override fun visitAttribute(attribute: Attribute) {
        if (attribute.type == InstrumentationEvidenceAttribute(diagnosticsPass).type) previousDiagnostics = attribute
        else if ((config.classGraph && (attribute.type == "JankHunterBuildEvidenceV1_class_graph" || attribute.type == "JankHunterBuildEvidenceV1_lambda_capture")) ||
            attribute.type == "JankHunterBuildEvidenceV1_android_component") {
            if (previousArtifacts == null) previousArtifacts = HashMap()
            previousArtifacts!![attribute.type] = attribute
        } else super.visitAttribute(attribute)
    }
    private val roomPolicy = RoomDaoInstrumentationPolicy(config.roomTracing, config.databaseTracing)
    private val classHierarchy = classHierarchy.mapTo(linkedSetOf()) { it.replace('.', '/') }
    private val androidComponentCatalog = AndroidComponentCatalogClassBuilder(className, this.classHierarchy)
    private val autoInitComponent = if (config.autoInit) {
        AutoInitComponent.fromHierarchy(this.classHierarchy)
    } else {
        null
    }
    private var kotlinGeneratedMethods = KotlinGeneratedMethodIndex.EMPTY
    private var superName: String? = null
    private var enclosingOwner: String? = null
    private var enclosingMethod: String? = null
    private var classAccess: Int = 0
    private val declaredMethods = hashSetOf<String>()
    private var autoInitMethodPresent = false
    private var alreadyInstrumented = false
    private var classHookApplied = false
    val hasAppliedHooks: Boolean get() = classHookApplied
    private var roomDatabaseFieldPresent = false
    private var plainFields: MutableMap<String, String>? = null
    private val taskBoundariesEnabled = config.executors || config.threadLifecycle || config.concurrencyDiagnostics
    private val taskLambdas = if (taskBoundariesEnabled) TaskLambdaIndex(className) else null
    private var deferredTaskMethods: MutableList<() -> Unit>? = null

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
        lambdaCaptures?.recordClass(access, name, superName, interfaces)
        androidComponentCatalog.recordClass(access)
        name?.let { classHierarchy.add(it.replace('.', '/')) }
        superName?.let {
            classHierarchy.add(it.replace('.', '/'))
            androidComponentCatalog.recordHierarchyType(it)
        }
        interfaces.orEmpty().forEach {
            classHierarchy.add(it.replace('.', '/'))
            androidComponentCatalog.recordHierarchyType(it)
        }
        super.visit(version, access, name, signature, superName, interfaces)
    }

    override fun visitOuterClass(owner: String?, name: String?, descriptor: String?) {
        enclosingOwner = owner
        enclosingMethod = name
        lambdaCaptures?.recordOuterClass(owner, name, descriptor)
        super.visitOuterClass(owner, name, descriptor)
    }

    override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
        val downstream = super.visitAnnotation(descriptor, visible)
        val delegate = lambdaCaptures?.annotationVisitor(descriptor, downstream) ?: downstream
        if (descriptor == instrumentationMarkerDescriptor) {
            alreadyInstrumented = true
            return delegate
        }
        if (descriptor == KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR) {
            return KotlinGeneratedMethodIndex.collectingVisitor(
                delegate, config.metadataCompatibilityMode, className, config.metadataDecodePathway,
            ) {
                kotlinGeneratedMethods = it
                diagnostics.recordKotlinMetadata(it)
            }
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
        lambdaCaptures?.recordField(access, name, descriptor)
        name?.let { androidComponentCatalog.recordField(access, it, descriptor, value) }
        if (name != null && config.methodFilterMode != JankHunterMethodFilterMode.NONE &&
            access and (Opcodes.ACC_VOLATILE or Opcodes.ACC_STATIC) == 0) {
            val fields = plainFields ?: HashMap<String, String>().also { plainFields = it }
            fields[name] = descriptor
        }
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
        declaredMethods.add(name + descriptor)
        androidComponentCatalog.recordMethod(access, name, descriptor)
        val next = super.visitMethod(access, name, descriptor, signature, exceptions)
        val autoInitEntryPoint = autoInitComponent?.matches(name, descriptor) == true
        val serviceCallback = if (
            config.androidComponents && AndroidServiceInstrumentationPolicy.isService(classHierarchy)
        ) {
            AndroidServiceInstrumentationPolicy.callback(name, descriptor)
        } else {
            null
        }
        val receiverCallback = if (
            config.androidComponents && AndroidBroadcastReceiverInstrumentationPolicy.isReceiver(classHierarchy)
        ) {
            AndroidBroadcastReceiverInstrumentationPolicy.callback(name, descriptor)
        } else {
            null
        }
        val binderServer = config.binderIPC &&
            AndroidBinderInstrumentationPolicy.isServer(classHierarchy) &&
            AndroidBinderInstrumentationPolicy.serverCallback(name, descriptor) != null
        val binderDescriptor = AndroidBinderInstrumentationPolicy.runtimeDescriptor(
            className,
            androidComponentCatalog.declaredAidlDescriptor(),
        )
        if (autoInitEntryPoint) autoInitMethodPresent = true
        if (alreadyInstrumented || (config.lifecycleLeaks && name + descriptor in existingLifecycleMethods)) {
            diagnostics.recordSkippedMethod("already_instrumented")
            return lambdaCaptureOnlyMethod(next, access, name, descriptor, signature, exceptions)
        }
        if (name == "<clinit>") {
            diagnostics.recordSkippedMethod("class_initializer")
            return lambdaCaptureOnlyMethod(next, access, name, descriptor, signature, exceptions)
        }
        if (access and Opcodes.ACC_ABSTRACT != 0) {
            diagnostics.recordSkippedMethod("abstract")
            return next
        }
        if (access and Opcodes.ACC_NATIVE != 0) {
            diagnostics.recordSkippedMethod("native")
            return next
        }
        val coroutineOwner = if (
            CoroutineStateMachineInstrumentationPolicy.matches(
                config.coroutines || config.concurrencyDiagnostics,
                name,
                descriptor,
                classHierarchy,
            )
        ) {
            OwnerIds.coroutineOwner(className, enclosingOwner, enclosingMethod)
        } else {
            null
        }
        val methodDiagnostics = InstrumentationDiagnosticsClassBuilder(className, diagnosticsPass)
        var methodHookApplied = false
        var componentHookApplied = false
        val commitHooks = {
            if (methodHookApplied) classHookApplied = true
            if (componentHookApplied) androidComponentCatalog.recordInstrumented(name, descriptor)
        }
        var coroutineEntryLine: Int? = null
        var trivialMethod: MethodFilterDecision? = null
        val instrument = {
                target: MethodVisitor,
                origins: List<DatabaseInvocationOrigin>,
                recordPriorityHandler: (Label) -> Unit,
            ->
            JankHunterMethodVisitor(
                target,
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
                methodDiagnostics,
                classOrigin = classOrigin,
                trivialMethod = trivialMethod,
                taskLambdaKind = taskLambdas?.kind(name, descriptor),
                autoInitComponent = autoInitComponent.takeIf { autoInitEntryPoint },
                recordClassHookApplied = { methodHookApplied = true },
                recordStaticEdge = { calleeOwner, calleeName ->
                    recordStaticEdge(name, descriptor, calleeOwner, calleeName)
                },
                roomDaoMethod = roomPolicy.isDaoBoundary(roomDatabaseFieldPresent, access, name),
                roomSqlBoundary = roomPolicy.sqlBoundary(roomDatabaseFieldPresent, access, name, descriptor),
                databaseInvocationOrigins = origins,
                recordPriorityHandler = recordPriorityHandler,
                serviceClass = config.androidComponents &&
                    AndroidServiceInstrumentationPolicy.isService(classHierarchy),
                serviceCallback = serviceCallback,
                recordServiceHookApplied = {
                    componentHookApplied = true
                    methodHookApplied = true
                },
                receiverCallback = receiverCallback,
                recordReceiverHookApplied = {
                    componentHookApplied = true
                    methodHookApplied = true
                },
                binderServer = binderServer,
                binderDescriptor = binderDescriptor,
                coroutineOwner = coroutineOwner,
                entrySourceLine = coroutineEntryLine,
                recordBinderHookApplied = {
                    componentHookApplied = true
                    methodHookApplied = true
                },
            )
        }
        if (!config.databaseTracing) {
            val captures = lambdaCaptures
            return object : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
                override fun visitEnd() {
                    super.visitEnd()
                    emitIndexedMethod(this, next) {
                        if (config.methodFilterMode != JankHunterMethodFilterMode.NONE) {
                            trivialMethod = TrivialMethodClassifier.classify(this, className, plainFields.orEmpty())
                        }
                        captures?.recordMethod(this)
                        coroutineEntryLine = instructions.toArray().filterIsInstance<LineNumberNode>()
                            .firstOrNull()?.line
                        if (config.concurrencyDiagnostics) {
                            val transformed = DatabasePriorityMethodNode(access, name, descriptor, signature, exceptions)
                            accept(instrument(transformed, emptyList(), transformed::recordPriorityHandler))
                            transformed.promotePriorityBlocks()
                            emitWithinCodeBudget(this, transformed, next, methodDiagnostics, commitHooks)
                        } else {
                            val transformed = MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions)
                            accept(instrument(transformed, emptyList()) {})
                            emitWithinCodeBudget(this, transformed, next, methodDiagnostics, commitHooks)
                        }
                    }
                }
            }
        }
        return object : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
            override fun visitEnd() {
                super.visitEnd()
                emitIndexedMethod(this, next) {
                    if (config.methodFilterMode != JankHunterMethodFilterMode.NONE) {
                        trivialMethod = TrivialMethodClassifier.classify(this, className, plainFields.orEmpty())
                    }
                    coroutineEntryLine = instructions.toArray().filterIsInstance<LineNumberNode>()
                        .firstOrNull()?.line
                    lambdaCaptures?.recordMethod(this)
                    val origins = analyzeDatabaseInvocationOrigins(className, this)
                    if (!config.concurrencyDiagnostics && !requiresDatabaseCatchPriority(name, descriptor, origins)) {
                        val transformed = MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions)
                        accept(instrument(transformed, origins) {})
                        emitWithinCodeBudget(this, transformed, next, methodDiagnostics, commitHooks)
                        return@emitIndexedMethod
                    }
                    val transformed = DatabasePriorityMethodNode(access, name, descriptor, signature, exceptions)
                    accept(instrument(transformed, origins, transformed::recordPriorityHandler))
                    transformed.promotePriorityBlocks()
                    emitWithinCodeBudget(this, transformed, next, methodDiagnostics, commitHooks)
                }
            }
        }
    }

    /** An oversized optional transform must never prevent the host application from building. */
    private fun emitWithinCodeBudget(
        original: MethodNode, transformed: MethodNode, next: MethodVisitor,
        methodDiagnostics: InstrumentationDiagnosticsClassBuilder, commitHooks: () -> Unit,
    ) {
        val size = CodeSizeEvaluator(null)
        transformed.accept(size)
        if (size.maxSize > 65_535 || transformed.maxLocals > 65_535) {
            diagnostics.recordSkippedMethod("code_size_budget")
            original.accept(next)
        } else {
            diagnostics.mergeMethod(methodDiagnostics.finish())
            commitHooks()
            transformed.accept(next)
        }
    }

    private fun requiresDatabaseCatchPriority(
        methodName: String,
        methodDescriptor: String,
        origins: List<DatabaseInvocationOrigin>,
    ): Boolean {
        return origins.any { origin ->
            val call = MethodCall(
                owner = origin.owner,
                name = origin.name,
                descriptor = origin.descriptor,
                caller = CallerMethod(className, methodName, methodDescriptor),
                ownerHierarchy = resolveOwnerHierarchy(origin.owner),
                databaseQuery = origin.normalizedLiteral,
                databaseQueryArgument = databaseQueryArgumentIndex(origin.owner, origin.name, origin.descriptor),
            )
            when (val intent = (HookIntentResolver.resolve(call, config) as? HookDecision.Matched)?.intent) {
                is HookIntent.DatabaseCall -> intent.statementAction != DatabaseStatementAction.REGISTER
                is HookIntent.DatabaseTransaction -> intent.action == DatabaseTransactionAction.END
                else -> false
            }
        }
    }

    private fun emitEvidence(kind: String, json: String) {
        val previous = previousArtifacts?.remove("JankHunterBuildEvidenceV1_$kind")
        super.visitAttribute(previous ?: InstrumentationEvidenceAttribute(kind, json))
    }

    override fun visitEnd() {
        deferredTaskMethods?.forEach { it() }
        deferredTaskMethods = null
        emitSyntheticLifecycleMethodsIfNeeded()
        emitSyntheticAutoInitMethodIfNeeded()
        if (!alreadyInstrumented && (!markerOnlyWhenHookApplied || classHookApplied)) {
            super.visitAnnotation(instrumentationMarkerDescriptor, false)?.visitEnd()
        }
        if (config.classGraph) {
            if (edges.isNotEmpty()) emitEvidence("class_graph", ClassGraphWriter.record(className.replace('/', '.'), edges))
            val captures = lambdaCaptures?.finish().orEmpty()
            if (captures.isNotEmpty()) emitEvidence("lambda_capture", LambdaCaptureWriter.records(className, captures))
            ClassGraphWriter.write(config.classGraphDirectory, className, edges)
            LambdaCaptureWriter.write(
                config.lambdaCaptureDirectory,
                className,
                captures,
            )
        }
        val hierarchyFailures = hierarchyResolutionDiagnostics()
        diagnostics.recordHierarchyResolutionFailures(hierarchyFailures)
        val metadataIncomplete = kotlinGeneratedMethods.analysisStatus != KotlinMetadataAnalysisStatus.READABLE &&
            kotlinGeneratedMethods.analysisStatus != KotlinMetadataAnalysisStatus.ABSENT
        if (metadataIncomplete || hierarchyFailures.isNotEmpty() || !diagnosticsOnlyWhenHookApplied || classHookApplied) {
            val record = diagnostics.finish()
            val evidence = if (alreadyInstrumented) previousDiagnostics else null
            super.visitAttribute(evidence ?: InstrumentationEvidenceAttribute(diagnosticsPass,
                InstrumentationDiagnosticsWriter.toJsonLine(record)))
            if (diagnosticsSink != null) diagnosticsSink.invoke(record)
            else InstrumentationDiagnosticsWriter.write(config.instrumentationDiagnosticsDirectory, record)
        } else {
            previousDiagnostics?.let { super.visitAttribute(it) }
        }
        val component = androidComponentCatalog.finish()
        component?.let { emitEvidence("android_component", AndroidComponentCatalogWriter.toJsonLine(it)) }
        AndroidComponentCatalogWriter.write(config.androidComponentCatalogDirectory, className, component)
        previousArtifacts?.values?.forEach { super.visitAttribute(it) }
        super.visitEnd()
    }

    private fun emitSyntheticLifecycleMethodsIfNeeded() {
        if (!config.lifecycleLeaks || alreadyInstrumented || classAccess and Opcodes.ACC_INTERFACE != 0) return
        val parent = superName ?: return
        // Only known framework declarations can safely be overridden without method metadata.
        // A custom ancestor may declare a final callback; its own instrumented declaration
        // remains responsible for inherited delivery to descendants.
        val callbacks = syntheticLifecycleCallbacks?.toList() ?: when (parent) {
            "android/app/Fragment", "androidx/fragment/app/Fragment" ->
                listOf("onDestroyView" to Opcodes.ACC_PUBLIC, "onDestroy" to Opcodes.ACC_PUBLIC)
            "androidx/lifecycle/ViewModel" -> listOf("onCleared" to Opcodes.ACC_PROTECTED)
            "android/app/Activity" -> listOf("onDestroy" to Opcodes.ACC_PROTECTED)
            "android/app/Service" -> listOf("onDestroy" to Opcodes.ACC_PUBLIC)
            else -> emptyList()
        }
        callbacks.forEach { (name, access) ->
            if (name + "()V" !in declaredMethods) {
                visitMethod(access, name, "()V", null, null).apply {
                    visitCode()
                    visitVarInsn(Opcodes.ALOAD, 0)
                    visitMethodInsn(Opcodes.INVOKESPECIAL, parent, name, "()V", false)
                    visitInsn(Opcodes.RETURN)
                    visitMaxs(1, 1)
                    visitEnd()
                }
            }
        }
    }

    private fun emitSyntheticAutoInitMethodIfNeeded() {
        val component = autoInitComponent ?: return
        val access = component.syntheticAccess ?: return
        val parent = superName ?: return
        if (alreadyInstrumented || autoInitMethodPresent) return
        if (classAccess and (Opcodes.ACC_ABSTRACT or Opcodes.ACC_INTERFACE) != 0) return

        visitMethod(access, component.methodName, component.methodDescriptor, null, null).apply {
            visitCode()
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

    private fun lambdaCaptureOnlyMethod(
        target: MethodVisitor,
        access: Int,
        name: String,
        descriptor: String,
        signature: String?,
        exceptions: Array<out String>?,
    ): MethodVisitor {
        val captures = lambdaCaptures
        if (captures == null && !taskBoundariesEnabled) return target
        return object : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
            override fun visitEnd() {
                super.visitEnd()
                taskLambdas?.record(this)
                captures?.recordMethod(this)
                accept(target)
            }
        }
    }

    private fun emitIndexedMethod(method: MethodNode, target: MethodVisitor, emit: () -> Unit) {
        taskLambdas?.record(method)
        val constructorSkipReason = originalConstructorReason(method)
        if (constructorSkipReason != null) {
            // Pre-initialization handlers or alternative super paths are unsafe for a single
            // method-wide catch-all, which would read uninitialized JH token locals.
            // Keep this uncommon constructor intact, including its original exception semantics.
            diagnostics.recordSkippedMethod(constructorSkipReason)
            lambdaCaptures?.recordMethod(method)
            method.accept(target)
            return
        }
        if (taskBoundariesEnabled && method.access and Opcodes.ACC_SYNTHETIC != 0) {
            val pending = deferredTaskMethods ?: ArrayList<() -> Unit>().also { deferredTaskMethods = it }
            pending.add(emit)
        } else {
            emit()
        }
    }

    private fun originalConstructorReason(method: MethodNode): String? {
        if (method.name != "<init>") return null
        var unknownFrame = false
        for (block in method.tryCatchBlocks) {
            var instruction = block.handler.next
            var frame: FrameNode? = null
            while (instruction != null && instruction.opcode < 0) {
                if (instruction is FrameNode) { frame = instruction; break }
                instruction = instruction.next
            }
            // Legacy classfiles without handler frames also stay unchanged; do not guess this's
            // initialization state. AGP supplies expanded frames for modern Kotlin/Java inputs.
            if (frame == null || frame.type != Opcodes.F_NEW || frame.local?.firstOrNull() != className) unknownFrame = true
            if (frame?.local.orEmpty().contains(Opcodes.UNINITIALIZED_THIS)) return "constructor_preinitialization_handler"
        }
        if (unknownFrame) return "constructor_handler_initialization_unknown"
        val initializationCandidates = method.instructions.asSequence().filterIsInstance<MethodInsnNode>().filter {
            it.opcode == Opcodes.INVOKESPECIAL && it.name == "<init>" && (it.owner == className || it.owner == superName)
        }.take(2).count()
        if (initializationCandidates > 1) {
            var initializationPaths = 0
            method.accept(object : AdviceAdapter(Opcodes.ASM9, null, method.access, method.name, method.desc) {
                override fun onMethodEnter() { initializationPaths++ }
            })
            if (initializationPaths > 1) return "constructor_multiple_initialization_paths"
        }
        return null
    }

    private companion object {
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
