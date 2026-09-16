package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter

internal class JankHunterMethodVisitor(
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
    private val coroutineOwner: String? = null,
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
    private val androidComponentEmitter = AndroidComponentMethodEmitter(
        visitor = this,
        state = state,
        className = className,
        methodName = methodName,
        serviceCallback = serviceCallback,
        binderDescriptor = binderDescriptor,
        recordServiceHookApplied = recordServiceHookApplied,
        recordReceiverHookApplied = recordReceiverHookApplied,
        recordBinderHookApplied = recordBinderHookApplied,
    )
    private var cachedCallerMethod: CallerMethod? = null

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
        if (coroutineOwner != null) {
            emitCoroutineSegmentEnter()
            recordClassHookApplied()
        }
        if (binderServer) androidComponentEmitter.binderServerEnter()
        if (receiverCallback == AndroidReceiverInvocation.RECEIVE) androidComponentEmitter.receiverCallbackEnter()
        serviceCallback?.let(androidComponentEmitter::serviceCallbackEnter)
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
        if (state.coroutineSegmentTokenLocal >= 0 && opcode == Opcodes.ARETURN) {
            dup()
            if (state.coroutineSegmentResultLocal < 0) {
                state.coroutineSegmentResultLocal = newLocal(Type.getType(Any::class.java))
            }
            storeLocal(state.coroutineSegmentResultLocal)
            emitCoroutineSegmentExit(throwableLocal = -1)
        }
        if (state.binderServerStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            androidComponentEmitter.captureBinderServerResult(opcode)
            androidComponentEmitter.binderServerExit(throwableLocal = -1)
        }
        if (state.receiverCallbackStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            androidComponentEmitter.receiverCallbackExit(failed = false)
        }
        if (state.serviceCallbackStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            androidComponentEmitter.captureServiceCallbackResult(opcode)
            androidComponentEmitter.serviceCallbackExit(failed = false)
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
                emitWorkerOutcomeClassification()
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

    private fun emitWorkerOutcomeClassification() {
        val failureCheck = newLabel()
        val retryCheck = newLabel()
        val unknown = newLabel()
        val classified = newLabel()

        dup()
        instanceOf(Type.getObjectType(ANDROIDX_WORK_RESULT_SUCCESS))
        ifZCmp(EQ, failureCheck)
        push(SEMANTIC_OUTCOME_SUCCESS)
        goTo(classified)

        mark(failureCheck)
        dup()
        instanceOf(Type.getObjectType(ANDROIDX_WORK_RESULT_FAILURE))
        ifZCmp(EQ, retryCheck)
        push(SEMANTIC_OUTCOME_FAILURE)
        goTo(classified)

        mark(retryCheck)
        dup()
        instanceOf(Type.getObjectType(ANDROIDX_WORK_RESULT_RETRY))
        ifZCmp(EQ, unknown)
        push(SEMANTIC_OUTCOME_RETRY)
        goTo(classified)

        mark(unknown)
        push(SEMANTIC_OUTCOME_UNKNOWN)
        mark(classified)
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
                    androidComponentEmitter.receiverGoAsync(
                        MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface),
                    )
                    recordReceiverHookApplied()
                    return
                }
                AndroidReceiverInvocation.FINISH_ASYNC -> {
                    androidComponentEmitter.receiverAsyncFinish(
                        MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface),
                    )
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
                androidComponentEmitter.binderClientTransaction(
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
                androidComponentEmitter.serviceForegroundCall(
                    MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface),
                    foregroundCall,
                )
                recordServiceHookApplied()
                return
            }
        }
        val namedCandidateMask = HookIntentResolver.namedCandidateMask(name)
        if (namedCandidateMask == 0) {
            HookNearMissDiagnostics.resolve(owner, name, descriptor, config)?.let { nearMiss ->
                diagnostics.recordDecision(nearMiss, methodDiagnosticName, state.currentLine)
            }
            super.visitMethodInsn(opcodeAndSource, owner, name, descriptor, isInterface)
            return
        }
        val staticDatabaseQuery = consumeDatabaseInvocationOrigin(owner, name, descriptor)
        val databaseQueryArgument = databaseQueryArgumentIndex(owner, name, descriptor)
        var call = MethodCall(
            owner = owner,
            name = name,
            descriptor = descriptor,
            caller = callerMethod(),
            line = state.currentLine,
            databaseQuery = staticDatabaseQuery,
            databaseQueryArgument = databaseQueryArgument,
        )
        val descriptorCandidateMask = HookIntentResolver.candidateMask(name, descriptor)
        val directCandidateMask = if (descriptorCandidateMask == 0) namedCandidateMask else descriptorCandidateMask
        var decision = HookIntentResolver.resolve(
            call,
            config,
            directCandidateMask,
        )
        if (decision is HookDecision.NotMatched && directCandidateMask != namedCandidateMask) {
            decision = HookIntentResolver.resolve(call, config, namedCandidateMask)
        }
        if (decision is HookDecision.NotMatched) {
            val ownerHierarchy = resolveOwnerHierarchy(owner)
            if (ownerHierarchy.isNotEmpty() && (ownerHierarchy.size != 1 || owner !in ownerHierarchy)) {
                call = call.copy(ownerHierarchy = ownerHierarchy)
                decision = HookIntentResolver.resolve(call, config, namedCandidateMask)
            }
        }
        if (
            decision is HookDecision.Matched &&
            decision.intent.requiresOkHttpHelper() &&
            !config.okHttpHelperAvailable
        ) {
            throw missingOkHttpHelper(call)
        }
        val matchedIntent = (decision as? HookDecision.Matched)?.intent
        if (matchedIntent != null) {
            val invocation = MethodInvocation(opcodeAndSource, owner, name, descriptor, isInterface)
            if (emitHook(matchedIntent, invocation)) {
                diagnostics.recordHook(decision, methodDiagnosticName, call.line)
                return
            }
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

    private fun callerMethod(): CallerMethod {
        val cached = cachedCallerMethod
        if (cached != null) return cached
        return CallerMethod(className, methodName, methodDescriptor).also { cachedCallerMethod = it }
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

    internal fun emitOriginalInvocation(invocation: MethodInvocation) {
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

    internal fun emitInvocationTryCatchBlock(start: Label, end: Label, handler: Label, type: String?) {
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
                androidComponentEmitter.serviceCallbackExit(failed = true)
            }
            if (state.receiverCallbackStartLocal >= 0) {
                androidComponentEmitter.receiverCallbackExit(failed = true)
            }
            if (state.binderServerStartLocal >= 0) {
                androidComponentEmitter.binderServerExit(throwableLocal)
            }
            if (state.coroutineSegmentTokenLocal >= 0) {
                emitCoroutineSegmentExit(throwableLocal)
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
        private const val JANK_HUNTER_RUNTIME = "io/jankhunter/runtime/JankHunter"
        private const val ANDROIDX_LISTENABLE_WORKER = "androidx/work/ListenableWorker"
        private const val ANDROIDX_WORK_RESULT_SUCCESS = "androidx/work/ListenableWorker${'$'}Result${'$'}Success"
        private const val ANDROIDX_WORK_RESULT_FAILURE = "androidx/work/ListenableWorker${'$'}Result${'$'}Failure"
        private const val ANDROIDX_WORK_RESULT_RETRY = "androidx/work/ListenableWorker${'$'}Result${'$'}Retry"
        private const val SEMANTIC_OUTCOME_SUCCESS = 0
        private const val SEMANTIC_OUTCOME_FAILURE = 1
        private const val SEMANTIC_OUTCOME_RETRY = 2
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
            || state.coroutineSegmentTokenLocal >= 0
    }

    private fun emitCoroutineSegmentEnter() {
        loadThis()
        visitLdcInsn(checkNotNull(coroutineOwner))
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "enterCoroutineSegment",
            "(Ljava/lang/Object;Ljava/lang/String;)J",
            false,
        )
        state.coroutineSegmentTokenLocal = newLocal(Type.LONG_TYPE)
        storeLocal(state.coroutineSegmentTokenLocal)
    }

    private fun emitCoroutineSegmentExit(throwableLocal: Int) {
        loadLocal(state.coroutineSegmentTokenLocal)
        loadThis()
        if (state.coroutineSegmentResultLocal >= 0 && throwableLocal < 0) {
            loadLocal(state.coroutineSegmentResultLocal)
        } else {
            visitInsn(Opcodes.ACONST_NULL)
        }
        if (throwableLocal >= 0) loadLocal(throwableLocal) else visitInsn(Opcodes.ACONST_NULL)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER_HOOKS,
            "exitCoroutineSegment",
            "(JLjava/lang/Object;Ljava/lang/Object;Ljava/lang/Throwable;)V",
            false,
        )
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
