package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.commons.AdviceAdapter
import java.io.File

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val params = parameters.get()
        val hookConfig = HookConfig(
            methodCounters = params.methodCounters.getOrElse(false),
            okhttp = params.okhttp.getOrElse(false),
            webSockets = params.webSockets.getOrElse(false),
            handlers = params.handlers.getOrElse(false),
            executors = params.executors.getOrElse(false),
            coroutines = params.coroutines.getOrElse(false),
            flowInteractions = params.flowInteractions.getOrElse(false),
            logSpam = params.logSpam.getOrElse(false),
            classGraph = params.classGraph.getOrElse(false),
            runtimeCallGraph = params.runtimeCallGraph.getOrElse(false),
            classGraphPath = params.classGraphPath.getOrElse(""),
            instrumentationDiagnosticsPath = params.instrumentationDiagnosticsPath.getOrElse(""),
        )
        if (params.asmProgressLog.getOrElse(false)) {
            AsmProgressReporter.recordInstrumented(
                params.progressLabel.getOrElse("unknown"),
                classContext.currentClassData.className,
                hookConfig.progressLabel(),
            )
        }
        return JankHunterClassVisitor(
            nextClassVisitor,
            classContext.currentClassData.className,
            hookConfig,
        )
    }

    override fun isInstrumentable(classData: ClassData): Boolean {
        val params = parameters.get()
        val hooksEnabled = params.methodCounters.getOrElse(false) ||
            params.okhttp.getOrElse(false) ||
            params.webSockets.getOrElse(false) ||
            params.handlers.getOrElse(false) ||
            params.executors.getOrElse(false) ||
            params.coroutines.getOrElse(false) ||
            params.flowInteractions.getOrElse(false) ||
            params.logSpam.getOrElse(false) ||
            params.classGraph.getOrElse(false) ||
            params.runtimeCallGraph.getOrElse(false)
        val matched = hooksEnabled && InstrumentationMatcher(
            params.includePackages.getOrElse(emptyList()),
            params.excludePackages.getOrElse(emptyList()),
            params.allowEmptyIncludePackages.getOrElse(false),
        ).matches(classData.className)
        if (params.asmProgressLog.getOrElse(false)) {
            AsmProgressReporter.recordScanned(
                params.progressLabel.getOrElse("unknown"),
                classData.className,
                matched,
            )
        }
        return matched
    }
}

internal data class HookConfig(
    val methodCounters: Boolean,
    val okhttp: Boolean,
    val webSockets: Boolean,
    val handlers: Boolean,
    val executors: Boolean,
    val coroutines: Boolean,
    val flowInteractions: Boolean,
    val logSpam: Boolean,
    val classGraph: Boolean,
    val runtimeCallGraph: Boolean,
    val classGraphPath: String,
    val instrumentationDiagnosticsPath: String,
) {
    fun progressLabel(): String {
        return buildList {
            if (methodCounters) add("methods")
            if (okhttp) add("okhttp")
            if (webSockets) add("websocket")
            if (handlers) add("handler")
            if (executors) add("executor")
            if (coroutines) add("coroutine")
            if (flowInteractions) add("flow")
            if (logSpam) add("logspam")
            if (classGraph) add("graph")
            if (runtimeCallGraph) add("runtimegraph")
        }.joinToString("+").ifEmpty { "none" }
    }
}

internal class JankHunterClassVisitor(
    next: ClassVisitor,
    private val className: String,
    private val config: HookConfig,
) : ClassVisitor(Opcodes.ASM9, next) {
    private val edges = linkedMapOf<ClassGraphEdgeKey, Int>()
    private val classAnnotations = JankAnnotationMetadata.Builder()
    private val diagnostics = InstrumentationDiagnosticsClassBuilder(className)

    override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
        val delegate = super.visitAnnotation(descriptor, visible)
        return JankAnnotationParser.visitorFor(descriptor, delegate, classAnnotations)
    }

    override fun visitMethod(
        access: Int,
        name: String,
        descriptor: String,
        signature: String?,
        exceptions: Array<out String>?,
    ): MethodVisitor {
        val next = super.visitMethod(access, name, descriptor, signature, exceptions)
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
            config,
            classAnnotations.snapshot(),
            name == "<init>",
            diagnostics,
        ) { calleeOwner, calleeName ->
            recordStaticEdge(name, descriptor, calleeOwner, calleeName)
        }
    }

    override fun visitEnd() {
        if (config.classGraph) {
            ClassGraphWriter.append(config.classGraphPath, className, edges)
        }
        InstrumentationDiagnosticsWriter.append(
            config.instrumentationDiagnosticsPath,
            diagnostics.finish(),
        )
        super.visitEnd()
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
    access: Int,
    private val methodName: String,
    private val methodDescriptor: String,
    private val className: String,
    private val config: HookConfig,
    private val classAnnotations: JankAnnotationMetadata,
    private val constructor: Boolean,
    private val diagnostics: InstrumentationDiagnosticsClassBuilder,
    private val recordStaticEdge: (String, String) -> Unit,
) : AdviceAdapter(Opcodes.ASM9, next, access, methodName, methodDescriptor) {
    private val generatedOwnerLabel = OwnerIds.ownerLabel(className, methodName, methodDescriptor)
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
            (!constructor && classAnnotations.owner != null)
    private val hookEmitter = HookBytecodeEmitter(this) { ownerLabel }
    private var runtimeCallStartLocal = -1
    private var annotationScopeLocal = -1
    private val methodTryStart = Label()
    private val methodTryEnd = Label()
    private val methodExceptionHandler = Label()
    private var currentLine: Int? = null

    override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
        val delegate = super.visitAnnotation(descriptor, visible)
        return JankAnnotationParser.visitorFor(descriptor, delegate, methodAnnotations)
    }

    override fun visitLineNumber(line: Int, start: Label) {
        currentLine = line
        super.visitLineNumber(line, start)
    }

    override fun onMethodEnter() {
        if (!shouldInstrumentMethod()) return
        if (hasAnnotationContext) {
            emitEnterAnnotatedContext()
        }
        if (config.methodCounters) {
            visitLdcInsn("owner.$ownerLabel")
            visitInsn(Opcodes.LCONST_1)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER,
                "recordCounter",
                "(Ljava/lang/String;J)V",
                false,
            )
        }
        if (config.runtimeCallGraph) {
            visitLdcInsn(ownerLabel)
            visitMethodInsn(
                Opcodes.INVOKESTATIC,
                JANK_HUNTER,
                "enterMethod",
                "(Ljava/lang/String;)J",
                false,
            )
            runtimeCallStartLocal = newLocal(Type.LONG_TYPE)
            storeLocal(runtimeCallStartLocal)
        }
        if (requiresCatchAllExit()) {
            visitLabel(methodTryStart)
        }
    }

    override fun onMethodExit(opcode: Int) {
        if (!shouldInstrumentMethod()) return
        if (config.runtimeCallGraph && runtimeCallStartLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitRuntimeCallExit()
        }
        if (annotationScopeLocal >= 0 && opcode != Opcodes.ATHROW) {
            emitExitAnnotatedContext()
        }
    }

    private fun emitRuntimeCallExit() {
        loadLocal(runtimeCallStartLocal)
        visitLdcInsn(ownerLabel)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "exitMethod",
            "(JLjava/lang/String;)V",
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
        val call = MethodCall(
            opcode = opcodeAndSource,
            owner = owner,
            name = name,
            descriptor = descriptor,
            isInterface = isInterface,
            caller = CallerMethod(className, methodName, methodDescriptor),
            line = currentLine,
        )
        val decision = HookIntentResolver.resolve(call, config)
        if (decision is HookDecision.Matched && emitHook(decision.intent)) {
            diagnostics.recordHook(decision, call.line)
            return
        }
        if (decision is HookDecision.Matched) {
            diagnostics.recordHook(decision, call.line)
        } else {
            diagnostics.recordDecision(
                if (decision is HookDecision.NotMatched) {
                    HookNearMissDiagnostics.resolve(call, config) ?: decision
                } else {
                    decision
                },
                call.line,
            )
        }
        super.visitMethodInsn(opcodeAndSource, owner, name, descriptor, isInterface)
    }

    private fun emitHook(intent: HookIntent): Boolean {
        val command = BytecodeCommandFactory.commandFor(intent)
        command.emit(hookEmitter)
        return command.replacesOriginalCall
    }

    override fun visitMaxs(maxStack: Int, maxLocals: Int) {
        if (shouldInstrumentMethod() && requiresCatchAllExit()) {
            visitLabel(methodTryEnd)
            visitTryCatchBlock(methodTryStart, methodTryEnd, methodExceptionHandler, null)
            visitLabel(methodExceptionHandler)
            val throwableLocal = newLocal(Type.getType(Throwable::class.java))
            storeLocal(throwableLocal)
            if (config.runtimeCallGraph && runtimeCallStartLocal >= 0) {
                emitRuntimeCallExit()
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
        diagnostics.recordMethod(
            ignored = ignored,
            annotation = if (!ignored) annotationDiagnosticKey() else null,
        )
        super.visitEnd()
    }

    companion object {
        private const val JANK_HUNTER = "io/jankhunter/runtime/JankHunter"
    }

    private fun emitEnterAnnotatedContext() {
        pushNullableString(annotationScreen)
        pushNullableString(ownerLabel)
        pushNullableString(annotationFlow)
        pushNullableString(annotationTrace)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "enterAnnotatedContext",
            "(Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;)Ljava/lang/Object;",
            false,
        )
        annotationScopeLocal = newLocal(Type.getType("Ljava/lang/Object;"))
        storeLocal(annotationScopeLocal)
    }

    private fun emitExitAnnotatedContext() {
        loadLocal(annotationScopeLocal)
        visitMethodInsn(
            Opcodes.INVOKESTATIC,
            JANK_HUNTER,
            "exitAnnotatedContext",
            "(Ljava/lang/Object;)V",
            false,
        )
    }

    private fun pushNullableString(value: String?) {
        if (value == null) {
            visitInsn(Opcodes.ACONST_NULL)
        } else {
            visitLdcInsn(value)
        }
    }

    private fun requiresCatchAllExit(): Boolean {
        return (config.runtimeCallGraph && runtimeCallStartLocal >= 0) || annotationScopeLocal >= 0
    }

    private fun instrumentationIgnored(): Boolean {
        return classAnnotations.ignored || methodAnnotations.ignored
    }

    private fun shouldInstrumentMethod(): Boolean {
        return !instrumentationIgnored() && (!constructor || constructorHasDirectAnnotationContext)
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
    private val preparedPaths = mutableSetOf<String>()

    @Synchronized
    fun prepare(path: String) {
        if (path.isBlank()) return
        val file = File(path)
        file.parentFile?.mkdirs()
        file.delete()
        preparedPaths.add(file.absolutePath)
    }

    @Synchronized
    fun append(path: String, className: String, edges: Map<ClassGraphEdgeKey, Int>) {
        if (path.isBlank() || edges.isEmpty()) return
        val file = File(path)
        file.parentFile?.mkdirs()
        if (preparedPaths.add(file.absolutePath) && file.exists()) {
            file.delete()
        }
        file.appendText(record(className.replace('/', '.'), edges))
    }

    fun isApplicationLike(owner: String): Boolean {
        return !InstrumentationPackages.isBuiltinExcluded(owner)
    }

    private fun record(className: String, edges: Map<ClassGraphEdgeKey, Int>): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.CLASS_GRAPH_FORMAT)
            append(",\"class\":\"")
            append(escape(className))
            append("\",\"edges\":[")
            edges.entries.forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append("{\"caller\":\"")
                append(escape(entry.key.caller))
                append("\",\"calleeClass\":\"")
                append(escape(entry.key.calleeClass))
                append("\",\"calleeMethod\":\"")
                append(escape(entry.key.calleeMethod))
                append("\",\"count\":")
                append(entry.value)
                append('}')
            }
            append("]}\n")
        }
    }

    private fun escape(value: String): String {
        return value
            .replace("\\", "\\\\")
            .replace("\"", "\\\"")
            .replace("\n", "\\n")
            .replace("\r", "\\r")
    }

}

internal object InstrumentationHooks {
    fun isOkHttpEventListenerFactory(owner: String, name: String, descriptor: String): Boolean {
        return VersionedBridgeCatalog.matchOkHttp(
            methodCall(owner, name, descriptor),
            setOf(HookIntent.WrapOkHttpEventListenerFactory.id),
        ) != null
    }

    fun isOkHttpBuild(owner: String, name: String, descriptor: String): Boolean {
        return VersionedBridgeCatalog.matchOkHttp(
            methodCall(owner, name, descriptor),
            setOf(HookIntent.InstallOkHttpEventListenerFactory.id),
        ) != null
    }

    fun isOkHttpNewWebSocket(owner: String, name: String, descriptor: String): Boolean {
        return VersionedBridgeCatalog.matchOkHttp(
            methodCall(owner, name, descriptor),
            setOf(HookIntent.WrapWebSocketListener.id),
        ) != null
    }

    fun handlerRunnableKind(owner: String, name: String, descriptor: String): HandlerRunnableKind? {
        return VersionedBridgeCatalog.matchHandler(methodCall(owner, name, descriptor))
            ?.intent
            ?.let { it as? HookIntent.HandlerRunnable }
            ?.kind
    }

    fun handlerRemoveCallbacksKind(owner: String, name: String, descriptor: String): HandlerRemoveCallbacksKind? {
        return VersionedBridgeCatalog.matchHandler(methodCall(owner, name, descriptor))
            ?.intent
            ?.let { it as? HookIntent.HandlerRemoveCallbacks }
            ?.kind
    }

    fun isHandlerRemoveCallbacksAndMessages(owner: String, name: String, descriptor: String): Boolean {
        return VersionedBridgeCatalog.matchHandler(methodCall(owner, name, descriptor))
            ?.intent == HookIntent.HandlerRemoveCallbacksAndMessages
    }

    fun isHandlerHasCallbacks(owner: String, name: String, descriptor: String): Boolean {
        return VersionedBridgeCatalog.matchHandler(methodCall(owner, name, descriptor))
            ?.intent == HookIntent.HandlerHasCallbacks
    }

    fun isHandlerMessageSend(owner: String, name: String, descriptor: String): Boolean {
        return VersionedBridgeCatalog.matchHandler(methodCall(owner, name, descriptor))
            ?.intent == HookIntent.HandlerMessageSend
    }

    fun executorRunnableKind(owner: String, name: String, descriptor: String): ExecutorRunnableKind? {
        return VersionedBridgeCatalog.matchExecutor(methodCall(owner, name, descriptor))
            ?.intent
            ?.let { it as? HookIntent.ExecutorRunnable }
            ?.kind
    }

    fun executorCallableKind(owner: String, name: String, descriptor: String): ExecutorCallableKind? {
        return VersionedBridgeCatalog.matchExecutor(methodCall(owner, name, descriptor))
            ?.intent
            ?.let { it as? HookIntent.ExecutorCallable }
            ?.kind
    }

    fun coroutineBlockKind(owner: String, name: String, descriptor: String): CoroutineBlockKind? {
        return VersionedBridgeCatalog.matchCoroutine(methodCall(owner, name, descriptor))
            ?.intent
            ?.let { it as? HookIntent.CoroutineBlock }
            ?.kind
    }

    fun isViewSetOnClickListener(owner: String, name: String, descriptor: String): Boolean {
        return VersionedBridgeCatalog.matchFlow(methodCall(owner, name, descriptor)) != null
    }

    fun logSpamLevel(owner: String, name: String, descriptor: String): Int? {
        return VersionedBridgeCatalog.matchLogSpam(methodCall(owner, name, descriptor))
            ?.intent
            ?.let { it as? HookIntent.LogSpam }
            ?.level
    }

    fun logSpamSource(owner: String, name: String, descriptor: String): String {
        return VersionedBridgeCatalog.matchLogSpam(methodCall(owner, name, descriptor))
            ?.intent
            ?.let { it as? HookIntent.LogSpam }
            ?.source
            ?: owner.replace('/', '.') + ".$name"
    }

    private fun methodCall(owner: String, name: String, descriptor: String): MethodCall {
        return MethodCall(
            opcode = Opcodes.INVOKEVIRTUAL,
            owner = owner,
            name = name,
            descriptor = descriptor,
            isInterface = false,
        )
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
    fun ownerLabel(className: String, methodName: String, descriptor: String): String {
        val normalizedClass = className.replace('/', '.')
        val readable = "$normalizedClass.$methodName"
        val id = fnv1a64("$readable$descriptor")
        return "$readable#${id.toString(16)}"
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
