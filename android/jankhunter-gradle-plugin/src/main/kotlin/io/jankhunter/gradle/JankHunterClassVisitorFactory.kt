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
import org.objectweb.asm.tree.MethodNode

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val params = parameters.get()
        val classData = classContext.currentClassData
        val selection = InstrumentationClassSelector(params).evaluate(classData)
        val hookConfig = InstrumentationHookConfigFactory.create(params)
        var visitor = nextClassVisitor
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
                    selection.runtime -> hookConfig
                    selection.networkBoundary || selection.databaseBoundary -> hookConfig.boundaryOnly(
                        network = selection.networkBoundary,
                        database = selection.databaseBoundary,
                    )
                    else -> hookConfig.autoInitOnly()
                },
                classHierarchy = hierarchyResolver?.resolve(classData.className).orEmpty(),
                resolveOwnerHierarchy = hierarchyResolver
                    ?.let { resolver -> resolver::resolve }
                    ?: { emptySet() },
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
        return InstrumentationClassSelector(parameters.get()).evaluate(classData).any()
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
    private val classAnnotations = JankAnnotationMetadata.Builder()
    private val diagnostics = InstrumentationDiagnosticsClassBuilder(className)
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

    override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
        val delegate = super.visitAnnotation(descriptor, visible)
        if (descriptor == instrumentationMarkerDescriptor) {
            alreadyInstrumented = true
            return delegate
        }
        if (
            descriptor == KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR &&
            config.methodFilterMode != JankHunterMethodFilterMode.NONE
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
        name?.let { androidComponentCatalog.recordField(access, it, descriptor, value) }
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
                diagnostics,
                autoInitComponent = autoInitComponent.takeIf { autoInitEntryPoint },
                recordClassHookApplied = { classHookApplied = true },
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
                    androidComponentCatalog.recordInstrumented(name, descriptor)
                    classHookApplied = true
                },
                receiverCallback = receiverCallback,
                recordReceiverHookApplied = {
                    androidComponentCatalog.recordInstrumented(name, descriptor)
                    classHookApplied = true
                },
                binderServer = binderServer,
                binderDescriptor = binderDescriptor,
                recordBinderHookApplied = {
                    androidComponentCatalog.recordInstrumented(name, descriptor)
                    classHookApplied = true
                },
            )
        }
        if (!config.databaseTracing) return instrument(next, emptyList()) {}
        return object : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
            override fun visitEnd() {
                super.visitEnd()
                val origins = analyzeDatabaseInvocationOrigins(className, this)
                if (!requiresDatabaseCatchPriority(name, descriptor, origins)) {
                    accept(instrument(next, origins) {})
                    return
                }
                val transformed = DatabasePriorityMethodNode(access, name, descriptor, signature, exceptions)
                accept(instrument(transformed, origins, transformed::recordPriorityHandler))
                transformed.promotePriorityBlocks()
                transformed.accept(next)
            }
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

    override fun visitEnd() {
        emitSyntheticAutoInitMethodIfNeeded()
        if (!alreadyInstrumented && (!markerOnlyWhenHookApplied || classHookApplied)) {
            super.visitAnnotation(instrumentationMarkerDescriptor, false)?.visitEnd()
        }
        if (config.classGraph) {
            ClassGraphWriter.write(config.classGraphDirectory, className, edges)
        }
        if (!diagnosticsOnlyWhenHookApplied || classHookApplied) {
            InstrumentationDiagnosticsWriter.write(
                config.instrumentationDiagnosticsDirectory,
                diagnostics.finish(),
            )
        }
        AndroidComponentCatalogWriter.write(
            config.androidComponentCatalogDirectory,
            className,
            androidComponentCatalog.finish(),
        )
        super.visitEnd()
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

private class JankHunterMethodVisitor(
    next: MethodVisitor,
    accessFlags: Int,
    private val methodName: String,
    private val methodDescriptor: String,
    private val className: String,
    classAccessFlags: Int,
    kotlinMethodOrigin: KotlinMethodOrigin,
    private val config: HookConfig,
    classAnnotations: JankAnnotationMetadata,
    private val constructor: Boolean,
    private val superName: String?,
    classHierarchy: Set<String>,
    private val resolveOwnerHierarchy: (String) -> Set<String>,
    private val diagnostics: InstrumentationDiagnosticsClassBuilder,
    private val autoInitComponent: AutoInitComponent?,
    private val recordClassHookApplied: () -> Unit,
    private val recordStaticEdge: (String, String) -> Unit,
    roomDaoMethod: Boolean = false,
    private val roomSqlBoundary: RoomSqlBoundary? = null,
    private val databaseInvocationOrigins: List<DatabaseInvocationOrigin> = emptyList(),
    private val recordPriorityHandler: (Label) -> Unit = {},
    private val serviceClass: Boolean = false,
    private val serviceCallback: AndroidServiceCallback? = null,
    private val recordServiceHookApplied: () -> Unit = {},
    private val receiverCallback: AndroidReceiverInvocation? = null,
    private val recordReceiverHookApplied: () -> Unit = {},
    private val binderServer: Boolean = false,
    private val binderDescriptor: String? = null,
    private val recordBinderHookApplied: () -> Unit = {},
) : AdviceAdapter(Opcodes.ASM9, next, accessFlags, methodName, methodDescriptor) {
    private val methodId = OwnerIds.methodId(className, methodName, methodDescriptor)
    private val generatedOwnerLabel = OwnerIds.readableOwner(className, methodName)
    private val methodDiagnosticName = "$methodName$methodDescriptor"
    private val annotationContext = MethodAnnotationContext(classAnnotations, constructor, generatedOwnerLabel)
    private val methodAnnotations = annotationContext.methodAnnotations
    private val semanticPolicy = SemanticInstrumentationPolicy(
        constructor,
        accessFlags,
        config.composeTracing,
        config.workerTracing,
        roomDaoMethod,
        methodName,
        methodDescriptor,
        classHierarchy,
    )
    private val lifecyclePolicy = LifecycleInstrumentationPolicy(
        enabled = config.lifecycleLeaks,
        constructor = constructor,
        staticMethod = accessFlags and Opcodes.ACC_STATIC != 0,
        methodName = methodName,
        methodDescriptor = methodDescriptor,
        hierarchy = classHierarchy,
    )
    private val hookEmitter = HookBytecodeEmitter(
        visitor = this,
        ownerLabel = { annotationContext.owner },
        ownerId = { methodId },
        emitOriginal = ::emitOriginalInvocation,
        emitTryCatchBlock = ::emitInvocationTryCatchBlock,
    )
    private val state = MethodInstrumentationState()
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
        state.currentLine = line
        super.visitLineNumber(line, start)
    }

    override fun onMethodEnter() {
        if (constructor) {
            // The JVM forbids using an uninitialized `this`. AdviceAdapter calls this only after
            // the first this()/super() invocation, which is the earliest safe constructor boundary.
            state.constructorBodyEntered = true
        }
        autoInitComponent?.let {
            emitAutoInit(it)
            recordClassHookApplied()
        }
        if (!shouldInstrumentMethod()) return
        if (binderServer) emitBinderServerEnter()
        if (receiverCallback == AndroidReceiverInvocation.RECEIVE) emitReceiverCallbackEnter()
        serviceCallback?.let(::emitServiceCallbackEnter)
        if (lifecyclePolicy.hookPoint == LifecycleHookPoint.ENTER) {
            emitLifecycleWatch()
        }
        if (annotationContext.hasContext) {
            emitEnterAnnotatedContext()
        }
        if (annotationContext.operation != null) {
            emitStartAnnotatedOperation()
        }
        if (config.methodCounters && methodBoundaryHooksEnabled()) {
            visitLdcInsn(methodId)
            visitLdcInsn(generatedOwnerLabel)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "recordMethodCall",
                "(JLjava/lang/String;)V",
                false,
            )
        }
        if (config.runtimeCallGraph && methodBoundaryHooksEnabled()) {
            visitLdcInsn(methodId)
            visitLdcInsn(generatedOwnerLabel)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "enterMethod",
                "(JLjava/lang/String;)J",
                false,
            )
            state.runtimeCallStartLocal = newLocal(Type.LONG_TYPE)
            storeLocal(state.runtimeCallStartLocal)
        }
        roomSqlBoundary?.let { boundary ->
            loadArg(boundary.queryArgument)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "normalizeDatabaseQuery",
                "(Ljava/lang/String;)Ljava/lang/String;",
                false,
            )
            state.databaseMethodQueryLocal = newLocal(Type.getType(String::class.java))
            storeLocal(state.databaseMethodQueryLocal)
            loadLocal(state.databaseMethodQueryLocal)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "databaseStatementFingerprint",
                "(Ljava/lang/String;)J",
                false,
            )
            state.databaseMethodFingerprintLocal = newLocal(Type.LONG_TYPE)
            storeLocal(state.databaseMethodFingerprintLocal)
            loadLocal(state.databaseMethodQueryLocal)
            push(boundary.operation.wireValue)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "databaseQueryOperation",
                "(Ljava/lang/String;I)I",
                false,
            )
            state.databaseMethodOperationLocal = newLocal(Type.INT_TYPE)
            storeLocal(state.databaseMethodOperationLocal)
            visitMethodInsn(Opcodes.INVOKESTATIC, JANK_HUNTER_HOOKS, "enterDatabase", "()J", false)
            state.databaseMethodStartLocal = newLocal(Type.LONG_TYPE)
            storeLocal(state.databaseMethodStartLocal)
        }
        state.semanticKind = semanticPolicy.select(annotationContext.composable)
        state.semanticKind?.let { kind ->
            if (kind == SemanticHookKind.WORKER) {
                emitWorkerEnter()
            } else {
                visitLdcInsn(kind.id)
                visitMethodInsn(
                    Opcodes.INVOKESTATIC,
                    JANK_HUNTER_HOOKS,
                    "enterSemantic",
                    "(I)J",
                    false,
                )
            }
            state.semanticStartLocal = newLocal(Type.LONG_TYPE)
            storeLocal(state.semanticStartLocal)
            if (kind == SemanticHookKind.WORKER) {
                state.semanticOutcomeLocal = newLocal(Type.INT_TYPE)
                visitLdcInsn(SEMANTIC_OUTCOME_UNKNOWN)
                storeLocal(state.semanticOutcomeLocal)
            }
        }
        if (requiresCatchAllExit()) {
            visitLabel(state.methodTryStart)
        }
    }

    private fun emitWorkerEnter() {
        loadThis()
        visitMethodInsn(
            Opcodes.INVOKEVIRTUAL,
            ANDROIDX_LISTENABLE_WORKER,
            "getId",
            "()Ljava/util/UUID;",
            false,
        )
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "workerInstanceId",
            "(Ljava/lang/Object;)J",
            false,
        )
        state.workerInstanceLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.workerInstanceLocal)

        loadThis()
        visitMethodInsn(
            Opcodes.INVOKEVIRTUAL,
            ANDROIDX_LISTENABLE_WORKER,
            "getRunAttemptCount",
            "()I",
            false,
        )
        state.workerRunAttemptLocal = newLocal(Type.INT_TYPE)
        storeLocal(state.workerRunAttemptLocal)

        loadLocal(state.workerInstanceLocal)
        visitLdcInsn(methodId)
        visitLdcInsn(generatedOwnerLabel)
        loadLocal(state.workerRunAttemptLocal)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "enterWorker",
            "(JJLjava/lang/String;I)J",
            false,
        )
    }

    override fun onMethodExit(opcode: Int) {
        if (!shouldInstrumentMethod()) return
        if (state.binderServerStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            captureBinderServerResult(opcode)
            emitBinderServerExit(throwableLocal = -1)
        }
        if (state.receiverCallbackStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitReceiverCallbackExit(failed = false)
        }
        if (state.serviceCallbackStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            captureServiceCallbackResult(opcode)
            emitServiceCallbackExit(failed = false)
        }
        if (lifecyclePolicy.hookPoint == LifecycleHookPoint.EXIT && opcode != Opcodes.ATHROW) {
            emitLifecycleWatch()
        }
        if (config.runtimeCallGraph && state.runtimeCallStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitRuntimeCallExit()
        }
        if (state.databaseMethodStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitDatabaseMethodExit(succeeded = true, throwableLocal = -1)
        }
        if (state.semanticStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            if (state.semanticOutcomeLocal >= 0 && opcode == Opcodes.ARETURN) {
                dup()
                visitMethodInsn(
                    Opcodes.INVOKESTATIC,
                    JANK_HUNTER_HOOKS,
                    "classifyWorkerOutcome",
                    "(Ljava/lang/Object;)I",
                    false,
                )
                storeLocal(state.semanticOutcomeLocal)
            }
            emitSemanticExit(outcome = SEMANTIC_OUTCOME_SUCCESS, outcomeLocal = state.semanticOutcomeLocal)
        }
        if (state.annotationOperationLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitFinishAnnotatedOperation(failed = false)
        }
        if (state.annotationScopeLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitExitAnnotatedContext()
        }
    }

    private fun emitRuntimeCallExit() {
        loadLocal(state.runtimeCallStartLocal)
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

    private fun emitDatabaseMethodExit(succeeded: Boolean, throwableLocal: Int) {
        if (roomSqlBoundary == null) return
        loadLocal(state.databaseMethodStartLocal)
        visitLdcInsn(methodId)
        visitLdcInsn(generatedOwnerLabel)
        loadLocal(state.databaseMethodQueryLocal)
        loadLocal(state.databaseMethodFingerprintLocal)
        push(DatabaseFrameworkKind.ROOM.wireValue)
        loadLocal(state.databaseMethodOperationLocal)
        push(DatabaseBoundaryKind.MATERIALIZE.wireValue)
        push(false)
        push(0)
        push(0)
        visitLdcInsn(0L)
        push(succeeded)
        if (throwableLocal >= 0) loadLocal(throwableLocal) else visitInsn(Opcodes.ACONST_NULL)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "exitDatabase",
            "(JJLjava/lang/String;Ljava/lang/String;JIIIZIIJZLjava/lang/Throwable;)V",
            false,
        )
    }

    private fun emitSemanticExit(outcome: Int, outcomeLocal: Int = -1) {
        val kind = state.semanticKind ?: return
        if (kind == SemanticHookKind.WORKER) {
            emitWorkerExit(outcome, outcomeLocal)
            return
        }
        loadLocal(state.semanticStartLocal)
        visitLdcInsn(kind.id)
        visitLdcInsn(methodId)
        visitLdcInsn(generatedOwnerLabel)
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

    private fun emitWorkerExit(outcome: Int, outcomeLocal: Int) {
        loadLocal(state.semanticStartLocal)
        loadLocal(state.workerInstanceLocal)
        visitLdcInsn(methodId)
        visitLdcInsn(generatedOwnerLabel)
        if (outcomeLocal >= 0) {
            loadLocal(outcomeLocal)
        } else {
            visitLdcInsn(outcome)
        }
        loadLocal(state.workerRunAttemptLocal)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "exitWorker",
            "(JJJLjava/lang/String;II)V",
            false,
        )
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
        if (config.androidComponents) {
            val receiverInvocation = AndroidBroadcastReceiverInstrumentationPolicy.invocation(owner, name, descriptor)
                ?: if (AndroidBroadcastReceiverInstrumentationPolicy.needsOwnerHierarchy(name, descriptor)) {
                    AndroidBroadcastReceiverInstrumentationPolicy.invocation(
                        owner,
                        name,
                        descriptor,
                        resolveOwnerHierarchy(owner),
                    )
                } else {
                    null
                }
            when (receiverInvocation) {
                AndroidReceiverInvocation.GO_ASYNC -> if (state.receiverCallbackStartLocal >= 0) {
                    emitReceiverGoAsync(MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface))
                    recordReceiverHookApplied()
                    return
                }
                AndroidReceiverInvocation.FINISH_ASYNC -> {
                    emitReceiverAsyncFinish(MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface))
                    recordClassHookApplied()
                    return
                }
                AndroidReceiverInvocation.RECEIVE,
                null,
                -> Unit
            }
        }
        if (config.binderIPC) {
            val binderInvocation = AndroidBinderInstrumentationPolicy.clientInvocation(owner, name, descriptor)
                ?: if (AndroidBinderInstrumentationPolicy.needsOwnerHierarchy(owner, name, descriptor)) {
                    AndroidBinderInstrumentationPolicy.clientInvocation(
                        owner,
                        name,
                        descriptor,
                        resolveOwnerHierarchy(owner),
                    )
                } else {
                    null
                }
            if (binderInvocation == AndroidBinderInvocation.CLIENT_TRANSACTION) {
                emitBinderClientTransaction(
                    MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface),
                )
                recordBinderHookApplied()
                return
            }
        }
        if (serviceClass) {
            val foregroundCall = AndroidServiceInstrumentationPolicy.foregroundCall(
                opcodeAndSource,
                owner,
                name,
                descriptor,
                emptySet(),
            ) ?: if (AndroidServiceInstrumentationPolicy.needsOwnerHierarchy(owner, name, descriptor)) {
                AndroidServiceInstrumentationPolicy.foregroundCall(
                    opcodeAndSource,
                    owner,
                    name,
                    descriptor,
                    resolveOwnerHierarchy(owner),
                )
            } else {
                null
            }
            if (foregroundCall != null) {
                emitServiceForegroundCall(
                    MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface),
                    foregroundCall,
                )
                recordServiceHookApplied()
                return
            }
        }
        val staticDatabaseQuery = consumeDatabaseInvocationOrigin(owner, name, descriptor)
        val databaseQueryArgument = databaseQueryArgumentIndex(owner, name, descriptor)
        val call = MethodCall(
            owner = owner,
            name = name,
            descriptor = descriptor,
            caller = CallerMethod(className, methodName, methodDescriptor),
            line = state.currentLine,
            ownerHierarchy = resolveOwnerHierarchy(owner),
            databaseQuery = staticDatabaseQuery,
            databaseQueryArgument = databaseQueryArgument,
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
        val matchedIntent = (decision as? HookDecision.Matched)?.intent
        if (matchedIntent != null && emitHook(matchedIntent, invocation)) {
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

    private fun consumeDatabaseInvocationOrigin(owner: String, name: String, descriptor: String): String? {
        val origin = databaseInvocationOrigins.getOrNull(state.databaseInvocationOriginIndex) ?: return null
        if (origin.owner != owner || origin.name != name || origin.descriptor != descriptor) return null
        state.databaseInvocationOriginIndex++
        return origin.normalizedLiteral
    }

    private fun HookIntent.requiresOkHttpHelper(): Boolean {
        return when (this) {
            HookIntent.WrapOkHttpEventListenerFactory,
            HookIntent.InstallOkHttpEventListener,
            HookIntent.InstallOkHttpEventListenerFactory,
            HookIntent.GuardOkHttpNewCall,
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
            is HookIntent.CriticalIO,
            is HookIntent.DatabaseCall,
            is HookIntent.DatabaseTransaction,
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
                "before enabling JankHunter NETWORK/WEBSOCKETS features. Instrumentation stopped before " +
                "emitting bytecode that could crash the host app.",
        )
    }

    private fun emitHook(intent: HookIntent, invocation: MethodInvocation): Boolean {
        val command = BytecodeCommandFactory.commandFor(intent)
        command.emit(hookEmitter, invocation)
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

    private fun emitInvocationTryCatchBlock(start: Label, end: Label, handler: Label, type: String?) {
        recordPriorityHandler(handler)
        emitPostSuperTryCatchBlock(start, end, handler, type)
    }

    override fun visitMaxs(maxStack: Int, maxLocals: Int) {
        if (shouldInstrumentMethod() && requiresCatchAllExit()) {
            visitLabel(state.methodTryEnd)
            if (constructor) {
                emitPostSuperTryCatchBlock(state.methodTryStart, state.methodTryEnd, state.methodExceptionHandler, null)
            } else {
                visitTryCatchBlock(state.methodTryStart, state.methodTryEnd, state.methodExceptionHandler, null)
            }
            visitLabel(state.methodExceptionHandler)
            val throwableLocal = newLocal(Type.getType(Throwable::class.java))
            storeLocal(throwableLocal)
            if (config.runtimeCallGraph && state.runtimeCallStartLocal >= 0) {
                emitRuntimeCallExit()
            }
            if (state.databaseMethodStartLocal >= 0) {
                emitDatabaseMethodExit(succeeded = false, throwableLocal = throwableLocal)
            }
            if (state.serviceCallbackStartLocal >= 0) {
                emitServiceCallbackExit(failed = true)
            }
            if (state.receiverCallbackStartLocal >= 0) {
                emitReceiverCallbackExit(failed = true)
            }
            if (state.binderServerStartLocal >= 0) {
                emitBinderServerExit(throwableLocal)
            }
            if (state.semanticStartLocal >= 0) {
                emitSemanticExit(outcome = SEMANTIC_OUTCOME_FAILURE)
            }
            if (state.annotationOperationLocal >= 0) {
                emitFinishAnnotatedOperation(failed = true)
            }
            if (state.annotationScopeLocal >= 0) {
                emitExitAnnotatedContext()
            }
            loadLocal(throwableLocal)
            mv.visitInsn(Opcodes.ATHROW)
        }
        super.visitMaxs(maxStack + 10, maxLocals)
    }

    override fun visitEnd() {
        val ignored = instrumentationIgnored()
        diagnostics.recordMethod(
            ignored = ignored,
            annotation = if (!ignored) annotationContext.diagnosticKey() else null,
        )
        if (config.methodFilterMode != JankHunterMethodFilterMode.NONE) {
            diagnostics.recordMethodFilter(
                methodFilterDecision,
                excluded = config.methodFilterMode == JankHunterMethodFilterMode.FILTER &&
                    methodFilterDecision.exclusionReason != null,
            )
        }
        super.visitEnd()
    }

    private companion object {
        private const val JANK_HUNTER_HOOKS = "io/jankhunter/runtime/JankHunterHooks"
        private const val JANK_HUNTER_ANDROID_HOOKS = "io/jankhunter/runtime/JankHunterAndroidHooks"
        private const val JANK_HUNTER_RUNTIME = "io/jankhunter/runtime/JankHunter"
        private const val ANDROIDX_LISTENABLE_WORKER = "androidx/work/ListenableWorker"
        private const val SEMANTIC_OUTCOME_SUCCESS = 0
        private const val SEMANTIC_OUTCOME_FAILURE = 1
        private const val SEMANTIC_OUTCOME_UNKNOWN = 4
        private const val RECEIVER_INTENT_ARGUMENT = 1
        private const val BINDER_CODE_ARGUMENT = 0
        private const val BINDER_FLAGS_ARGUMENT = 3
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
        pushNullableString(annotationContext.screen)
        pushNullableString(annotationContext.owner)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "enterAnnotatedContext",
            "(Ljava/lang/String;Ljava/lang/String;)Ljava/lang/Object;",
            false,
        )
        state.annotationScopeLocal = newLocal(Type.getType("Ljava/lang/Object;"))
        storeLocal(state.annotationScopeLocal)
    }

    private fun emitStartAnnotatedOperation() {
        pushNullableString(annotationContext.operation)
        visitLdcInsn(annotationOperationKindWireValue(annotationContext.operationKind))
        visitLdcInsn(annotationContext.operationBudgetMs)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "startAnnotatedOperation",
            "(Ljava/lang/String;IJ)Ljava/lang/Object;",
            false,
        )
        state.annotationOperationLocal = newLocal(Type.getType("Ljava/lang/Object;"))
        storeLocal(state.annotationOperationLocal)
    }

    private fun emitFinishAnnotatedOperation(failed: Boolean) {
        loadLocal(state.annotationOperationLocal)
        if (constructor) {
            mv.visitInsn(if (failed) Opcodes.ICONST_1 else Opcodes.ICONST_0)
            mv.visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "finishAnnotatedOperation",
                "(Ljava/lang/Object;Z)V",
                false,
            )
        } else {
            push(failed)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER_HOOKS,
                "finishAnnotatedOperation",
                "(Ljava/lang/Object;Z)V",
                false,
            )
        }
    }

    private fun emitExitAnnotatedContext() {
        loadLocal(state.annotationScopeLocal)
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
        return (config.runtimeCallGraph && state.runtimeCallStartLocal >= 0) || state.annotationScopeLocal >= 0 ||
            state.annotationOperationLocal >= 0 ||
            state.semanticStartLocal >= 0 || state.databaseMethodStartLocal >= 0
            || state.serviceCallbackStartLocal >= 0
            || state.receiverCallbackStartLocal >= 0
            || state.binderServerStartLocal >= 0
    }

    private fun instrumentationIgnored(): Boolean {
        return annotationContext.ignored
    }

    private fun shouldInstrumentMethod(): Boolean {
        return !instrumentationIgnored() && (!constructor || state.constructorBodyEntered)
    }

    private fun methodBoundaryHooksEnabled(): Boolean {
        return config.methodFilterMode != JankHunterMethodFilterMode.FILTER ||
            methodFilterDecision.exclusionReason == null
    }

    private fun emitLifecycleWatch() {
        loadThis()
        visitLdcInsn(methodName)
        visitLdcInsn(annotationContext.owner)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "watchLifecycleObject",
            "(Ljava/lang/Object;Ljava/lang/String;Ljava/lang/String;)V",
            false,
        )
        diagnostics.recordLifecycleHook(methodName, methodDescriptor, superName)
        recordClassHookApplied()
    }

    private fun emitServiceCallbackEnter(callback: AndroidServiceCallback) {
        when (callback.resultKind) {
            AndroidServiceResultKind.INT -> {
                state.serviceResultCodeLocal = newLocal(Type.INT_TYPE)
                push(Int.MIN_VALUE)
                storeLocal(state.serviceResultCodeLocal)
            }
            AndroidServiceResultKind.OBJECT -> {
                state.serviceResultObjectLocal = newLocal(Type.getType(Any::class.java))
                visitInsn(Opcodes.ACONST_NULL)
                storeLocal(state.serviceResultObjectLocal)
            }
            AndroidServiceResultKind.NONE -> Unit
        }
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "enterServiceCallback",
            "()J",
            false,
        )
        state.serviceCallbackStartLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.serviceCallbackStartLocal)
        recordServiceHookApplied()
    }

    private fun captureServiceCallbackResult(opcode: Int) {
        when {
            state.serviceResultCodeLocal >= 0 && opcode == Opcodes.IRETURN -> {
                dup()
                storeLocal(state.serviceResultCodeLocal)
            }
            state.serviceResultObjectLocal >= 0 && opcode == Opcodes.ARETURN -> {
                dup()
                storeLocal(state.serviceResultObjectLocal)
            }
        }
    }

    private fun emitServiceCallbackExit(failed: Boolean) {
        val callback = serviceCallback ?: return
        loadLocal(state.serviceCallbackStartLocal)
        loadThis()
        if (callback.intentArgument >= 0) loadArg(callback.intentArgument) else visitInsn(Opcodes.ACONST_NULL)
        visitLdcInsn(OwnerIds.methodId(className, "<android-component>", "service"))
        visitLdcInsn(className.replace('/', '.'))
        push(callback.stage)
        if (state.serviceResultCodeLocal >= 0) loadLocal(state.serviceResultCodeLocal) else push(Int.MIN_VALUE)
        if (state.serviceResultObjectLocal >= 0) {
            loadLocal(state.serviceResultObjectLocal)
        } else {
            visitInsn(Opcodes.ACONST_NULL)
        }
        push(failed)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "exitServiceCallback",
            "(JLjava/lang/Object;Ljava/lang/Object;JLjava/lang/String;IILjava/lang/Object;Z)V",
            false,
        )
    }

    private fun emitServiceForegroundCall(
        invocation: MethodInvocation,
        foregroundCall: AndroidServiceForegroundCall,
    ) {
        val argumentTypes = Type.getArgumentTypes(invocation.descriptor)
        val argumentLocals = IntArray(argumentTypes.size)
        for (index in argumentTypes.indices.reversed()) {
            val type = argumentTypes[index]
            argumentLocals[index] = newLocal(type)
            storeLocal(argumentLocals[index], type)
        }
        val receiverLocal = if (foregroundCall.serviceArgument == AndroidServiceForegroundCall.INVOCATION_RECEIVER) {
            newLocal(Type.getObjectType(invocation.owner)).also { storeLocal(it) }
        } else {
            -1
        }
        if (receiverLocal >= 0) loadLocal(receiverLocal)
        argumentTypes.indices.forEach { index -> loadLocal(argumentLocals[index], argumentTypes[index]) }
        emitOriginalInvocation(invocation)
        if (receiverLocal >= 0) {
            loadLocal(receiverLocal)
        } else {
            loadLocal(argumentLocals[foregroundCall.serviceArgument])
        }
        visitLdcInsn(OwnerIds.methodId(className, "<android-component>", "service"))
        visitLdcInsn(className.replace('/', '.'))
        push(foregroundCall.stage)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "recordServiceForegroundTransition",
            "(Ljava/lang/Object;JLjava/lang/String;I)V",
            false,
        )
    }

    private fun emitReceiverCallbackEnter() {
        state.receiverAsyncStartedLocal = newLocal(Type.BOOLEAN_TYPE)
        push(false)
        storeLocal(state.receiverAsyncStartedLocal)
        state.receiverPendingResultLocal = newLocal(Type.getType(Any::class.java))
        visitInsn(Opcodes.ACONST_NULL)
        storeLocal(state.receiverPendingResultLocal)
        loadThis()
        loadArg(RECEIVER_INTENT_ARGUMENT)
        visitLdcInsn(OwnerIds.methodId(className, "<android-component>", "receiver"))
        visitLdcInsn(className.replace('/', '.'))
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "enterReceiverCallback",
            "(Ljava/lang/Object;Ljava/lang/Object;JLjava/lang/String;)J",
            false,
        )
        state.receiverCallbackStartLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.receiverCallbackStartLocal)
        recordReceiverHookApplied()
    }

    private fun emitReceiverCallbackExit(failed: Boolean) {
        loadLocal(state.receiverCallbackStartLocal)
        loadThis()
        loadArg(RECEIVER_INTENT_ARGUMENT)
        visitLdcInsn(OwnerIds.methodId(className, "<android-component>", "receiver"))
        visitLdcInsn(className.replace('/', '.'))
        loadLocal(state.receiverAsyncStartedLocal)
        loadLocal(state.receiverPendingResultLocal)
        push(failed)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "exitReceiverCallback",
            "(JLjava/lang/Object;Ljava/lang/Object;JLjava/lang/String;ZLjava/lang/Object;Z)V",
            false,
        )
    }

    private fun emitReceiverGoAsync(invocation: MethodInvocation) {
        emitOriginalInvocation(invocation)
        dup()
        storeLocal(state.receiverPendingResultLocal)
        loadLocal(state.receiverPendingResultLocal)
        loadThis()
        loadArg(RECEIVER_INTENT_ARGUMENT)
        loadLocal(state.receiverCallbackStartLocal)
        visitLdcInsn(OwnerIds.methodId(className, "<android-component>", "receiver"))
        visitLdcInsn(className.replace('/', '.'))
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "registerReceiverAsync",
            "(Ljava/lang/Object;Ljava/lang/Object;Ljava/lang/Object;JJLjava/lang/String;)Z",
            false,
        )
        storeLocal(state.receiverAsyncStartedLocal)
    }

    private fun emitReceiverAsyncFinish(invocation: MethodInvocation) {
        val pendingResultLocal = newLocal(Type.getObjectType(invocation.owner))
        storeLocal(pendingResultLocal)
        loadLocal(pendingResultLocal)
        emitOriginalInvocation(invocation)
        loadLocal(pendingResultLocal)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "finishReceiverAsync",
            "(Ljava/lang/Object;)V",
            false,
        )
    }

    private fun emitBinderServerEnter() {
        state.binderServerResultLocal = newLocal(Type.BOOLEAN_TYPE)
        push(false)
        storeLocal(state.binderServerResultLocal)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "enterBinderServer",
            "()J",
            false,
        )
        state.binderServerStartLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.binderServerStartLocal)
        recordBinderHookApplied()
    }

    private fun captureBinderServerResult(opcode: Int) {
        if (opcode != Opcodes.IRETURN) return
        dup()
        storeLocal(state.binderServerResultLocal)
    }

    private fun emitBinderServerExit(throwableLocal: Int) {
        loadLocal(state.binderServerStartLocal)
        pushNullableString(binderDescriptor)
        visitInsn(Opcodes.ACONST_NULL)
        loadArg(BINDER_CODE_ARGUMENT)
        loadArg(BINDER_FLAGS_ARGUMENT)
        loadLocal(state.binderServerResultLocal)
        if (throwableLocal >= 0) loadLocal(throwableLocal) else visitInsn(Opcodes.ACONST_NULL)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "exitBinderServer",
            "(JLjava/lang/String;Ljava/lang/String;IIZLjava/lang/Throwable;)V",
            false,
        )
    }

    private fun emitBinderClientTransaction(invocation: MethodInvocation) {
        val argumentTypes = Type.getArgumentTypes(invocation.descriptor)
        val argumentLocals = IntArray(argumentTypes.size)
        for (index in argumentTypes.indices.reversed()) {
            val type = argumentTypes[index]
            argumentLocals[index] = newLocal(type)
            storeLocal(argumentLocals[index], type)
        }
        val receiverLocal = newLocal(Type.getObjectType(invocation.owner))
        storeLocal(receiverLocal)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "enterBinderClient",
            "()J",
            false,
        )
        val tokenLocal = newLocal(Type.LONG_TYPE)
        storeLocal(tokenLocal)
        val start = Label()
        val end = Label()
        val handler = Label()
        val done = Label()
        visitLabel(start)
        loadLocal(receiverLocal)
        argumentTypes.indices.forEach { index -> loadLocal(argumentLocals[index], argumentTypes[index]) }
        emitOriginalInvocation(invocation)
        val resultLocal = newLocal(Type.BOOLEAN_TYPE)
        storeLocal(resultLocal)
        visitLabel(end)
        emitInvocationTryCatchBlock(start, end, handler, "java/lang/Throwable")
        emitBinderClientExit(tokenLocal, argumentLocals, resultLocal, throwableLocal = -1)
        loadLocal(resultLocal)
        goTo(done)
        visitLabel(handler)
        val throwableLocal = newLocal(Type.getType(Throwable::class.java))
        storeLocal(throwableLocal)
        emitBinderClientExit(tokenLocal, argumentLocals, resultLocal = -1, throwableLocal = throwableLocal)
        loadLocal(throwableLocal)
        visitInsn(Opcodes.ATHROW)
        visitLabel(done)
    }

    private fun emitBinderClientExit(
        tokenLocal: Int,
        argumentLocals: IntArray,
        resultLocal: Int,
        throwableLocal: Int,
    ) {
        loadLocal(tokenLocal)
        pushNullableString(binderDescriptor)
        pushNullableString(AndroidBinderInstrumentationPolicy.runtimeMethod(className, methodName))
        loadLocal(argumentLocals[BINDER_CODE_ARGUMENT])
        loadLocal(argumentLocals[BINDER_FLAGS_ARGUMENT])
        if (resultLocal >= 0) loadLocal(resultLocal) else push(false)
        if (throwableLocal >= 0) loadLocal(throwableLocal) else visitInsn(Opcodes.ACONST_NULL)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_ANDROID_HOOKS,
            "exitBinderClient",
            "(JLjava/lang/String;Ljava/lang/String;IIZLjava/lang/Throwable;)V",
            false,
        )
    }

}

private fun annotationOperationKindWireValue(value: String): Int {
    return when (value) {
        "SCREEN" -> 2
        "BACKGROUND" -> 3
        "SYSTEM" -> 4
        "STAGE" -> 5
        else -> 1
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
