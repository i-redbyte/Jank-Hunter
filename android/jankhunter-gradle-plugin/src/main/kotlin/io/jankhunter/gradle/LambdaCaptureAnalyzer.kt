package io.jankhunter.gradle

import org.objectweb.asm.Handle
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type
import org.objectweb.asm.tree.AbstractInsnNode
import org.objectweb.asm.tree.AnnotationNode
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.FieldInsnNode
import org.objectweb.asm.tree.InvokeDynamicInsnNode
import org.objectweb.asm.tree.LineNumberNode
import org.objectweb.asm.tree.MethodInsnNode
import org.objectweb.asm.tree.MethodNode
import org.objectweb.asm.tree.TypeInsnNode
import org.objectweb.asm.tree.analysis.Analyzer
import org.objectweb.asm.tree.analysis.SourceInterpreter
import org.objectweb.asm.tree.analysis.SourceValue

internal data class LambdaCapturedValue(
    val type: String,
    val role: String,
    val strength: String,
)

internal data class LambdaCapture(
    val callsiteId: String,
    val owner: String,
    val implementation: String,
    val functionalInterface: String,
    val representation: String,
    val values: List<LambdaCapturedValue>,
    val sinks: List<String>,
    val line: Int? = null,
    val desugared: Boolean = false,
    val weakDereference: String? = null,
    val suppressed: Boolean = false,
    val suppressionReason: String? = null,
    internal val weakImplementationKey: String? = null,
)

/**
 * Build-time only analysis. It observes original bytecode and never adds instructions to the app,
 * so lambda invocation and allocation paths keep exactly the same runtime cost.
 */
internal object LambdaCaptureAnalyzer {
    fun analyze(node: ClassNode): List<LambdaCapture> {
        val result = ArrayList<LambdaCapture>()
        val forcedUnwraps = HashSet<String>()
        val classSuppression = suppression(node.visibleAnnotations, node.invisibleAnnotations)
        node.methods.orEmpty().forEach { method ->
            val analysis = inspectMethod(node.name, method, classSuppression)
            result += analysis.captures
            analysis.forceUnwrapImplementationKey?.let(forcedUnwraps::add)
        }
        analyzeClassBased(node, classSuppression)?.let(result::add)
        applyWeakForceUnwrap(result, forcedUnwraps)
        result.sortBy(LambdaCapture::callsiteId)
        return result
    }

    internal fun analyzeMethod(
        className: String,
        method: MethodNode,
        classSuppression: LambdaCaptureSuppression?,
    ): List<LambdaCapture> = inspectMethod(className, method, classSuppression).captures

    internal fun inspectMethod(
        className: String,
        method: MethodNode,
        classSuppression: LambdaCaptureSuppression?,
    ): MethodAnalysis {
        val instructionList = method.instructions ?: return MethodAnalysis.EMPTY
        var hasLambda = false
        var hasWeakGet = false
        var hasForceUnwrap = false
        var cursor = instructionList.first
        while (cursor != null) {
            hasLambda = hasLambda || isLambdaMetafactoryCall(cursor)
            val call = cursor as? MethodInsnNode
            if (call != null) {
                hasWeakGet = hasWeakGet || isWeakGet(call)
                hasForceUnwrap = hasForceUnwrap || LambdaSinkCatalog.isForceUnwrap(call)
            }
            cursor = cursor.next
        }
        if (!hasLambda && (!hasWeakGet || !hasForceUnwrap)) return MethodAnalysis.EMPTY
        val instructions = instructionList.toArray()
        var captures: LinkedHashMap<InvokeDynamicInsnNode, MutableLambdaCapture>? = null
        var hasPotentialSink = false
        if (hasLambda) {
            val methodSuppression = suppression(method.visibleAnnotations, method.invisibleAnnotations) ?: classSuppression
            var currentLine: Int? = null
            var ordinal = 0
            instructions.forEach { instruction ->
                hasPotentialSink = hasPotentialSink || LambdaSinkCatalog.mayConsumeLambda(instruction)
                if (instruction is LineNumberNode) currentLine = instruction.line
                val dynamic = instruction as? InvokeDynamicInsnNode ?: return@forEach
                if (!isLambdaMetafactoryCall(dynamic)) return@forEach
                val capturedTypes = Type.getArgumentTypes(dynamic.desc)
                if (capturedTypes.isEmpty()) return@forEach
                val implementation = dynamic.bsmArgs.firstNotNullOfOrNull { it as? Handle }
                val implementationOwner = implementation?.owner ?: className
                val implementationName = implementation?.name ?: dynamic.name
                val implementationDescriptor = implementation?.desc ?: dynamic.desc
                val callsiteID = OwnerIds.canonical(
                    OwnerIds.methodId(
                        className,
                        "${method.name}@${currentLine ?: 0}@$ordinal@$implementationOwner.$implementationName",
                        "${method.desc}|$implementationDescriptor|${dynamic.desc}",
                    ),
                )
                ordinal++
                val values = capturedTypes.map { type ->
                    LambdaCapturedValue(
                        type = readableType(type),
                        role = "capture",
                        strength = captureStrength(type),
                    )
                }
                val currentCaptures = captures
                    ?: LinkedHashMap<InvokeDynamicInsnNode, MutableLambdaCapture>().also { captures = it }
                currentCaptures[dynamic] = MutableLambdaCapture(
                    callsiteId = callsiteID,
                    owner = "${className.replace('/', '.')}.${method.name}${method.desc}",
                    implementation = "${implementationOwner.replace('/', '.')}.$implementationName",
                    functionalInterface = Type.getReturnType(dynamic.desc).className,
                    values = values,
                    line = currentLine,
                    weakImplementationKey = if (
                        values.any { it.strength == "weak" } && implementationOwner == className
                    ) {
                        implementationKey(implementationOwner, implementationName, implementationDescriptor)
                    } else {
                        null
                    },
                    suppression = methodSuppression,
                )
            }
        }
        val needsWeakAnalysis = hasWeakGet && hasForceUnwrap
        if (captures == null && !needsWeakAnalysis) return MethodAnalysis.EMPTY
        val frames = if (needsWeakAnalysis || captures != null && hasPotentialSink) {
            bestEffortAsmAnalysis {
                Analyzer(LambdaSourceInterpreter()).analyze(className, method)
            }
        } else {
            null
        }
        if (captures != null && hasPotentialSink && frames != null) {
            associateSinks(instructions, frames, captures)
        }
        val forceUnwrapImplementationKey = if (
            needsWeakAnalysis && frames != null && hasWeakForceUnwrap(instructions, frames)
        ) {
            implementationKey(className, method.name, method.desc)
        } else {
            null
        }
        return MethodAnalysis(
            captures = captures?.values?.map(MutableLambdaCapture::finish).orEmpty(),
            forceUnwrapImplementationKey = forceUnwrapImplementationKey,
        )
    }

    private fun associateSinks(
        instructions: Array<out AbstractInsnNode>,
        frames: Array<out org.objectweb.asm.tree.analysis.Frame<SourceValue>?>,
        captures: Map<InvokeDynamicInsnNode, MutableLambdaCapture>,
    ) {
        for (index in instructions.indices) {
            val instruction = instructions[index]
            when (instruction) {
                is MethodInsnNode -> {
                    val sink = LambdaSinkCatalog.sink(instruction)
                    val flowLaunch = LambdaSinkCatalog.isFlowLaunchIn(instruction)
                    val coroutineBuilder = LambdaSinkCatalog.isCoroutineBuilder(instruction)
                    if (sink == null && !flowLaunch && !coroutineBuilder) continue
                    val lambdaSources = sourceLambdas(instruction, frames[index])
                    if (sink != null) {
                        lambdaSources.forEach { source ->
                            captures[source]?.addSink(sink)
                        }
                    }
                    if (flowLaunch) {
                        val flowSources = argumentSources(instruction, frames[index], argumentFromEnd = 1)
                            .filterIsInstance<InvokeDynamicInsnNode>()
                        val scopeSink = when {
                            argumentHasSource(
                                instruction,
                                frames[index],
                                argumentFromEnd = 0,
                                LambdaSinkCatalog::isGlobalScopeSource,
                            ) -> "flow.global_scope"
                            argumentHasSource(
                                instruction,
                                frames[index],
                                argumentFromEnd = 0,
                                LambdaSinkCatalog::isLifecycleScopeSource,
                            ) -> "flow.lifecycle_scope"
                            else -> null
                        }
                        if (scopeSink != null) {
                            flowSources.forEach { source -> captures[source]?.addSink(scopeSink) }
                        }
                    }
                    if (
                        coroutineBuilder && consumedHasSource(
                            instruction,
                            frames[index],
                            LambdaSinkCatalog::isGlobalScopeSource,
                        )
                    ) {
                        lambdaSources.forEach { source -> captures[source]?.addSink("coroutine.global_scope") }
                    }
                }
                is FieldInsnNode -> {
                    val sink = when (instruction.opcode) {
                        Opcodes.PUTSTATIC -> "static.field"
                        Opcodes.PUTFIELD -> "instance.field"
                        else -> null
                    }
                    if (sink != null) {
                        valueSources(frames[index]).forEach { source -> captures[source]?.addSink(sink) }
                    }
                }
            }
        }
    }

    private fun sourceLambdas(
        invocation: MethodInsnNode,
        frame: org.objectweb.asm.tree.analysis.Frame<SourceValue>?,
    ): Set<InvokeDynamicInsnNode> {
        if (frame == null) return emptySet()
        val first = firstConsumedStackIndex(invocation, frame) ?: return emptySet()
        val result = linkedSetOf<InvokeDynamicInsnNode>()
        for (stackIndex in first until frame.stackSize) {
            frame.getStack(stackIndex).insns.filterIsInstanceTo(result)
        }
        return result
    }

    private fun consumedHasSource(
        invocation: MethodInsnNode,
        frame: org.objectweb.asm.tree.analysis.Frame<SourceValue>?,
        predicate: (AbstractInsnNode) -> Boolean,
    ): Boolean {
        if (frame == null) return false
        val first = firstConsumedStackIndex(invocation, frame) ?: return false
        for (stackIndex in first until frame.stackSize) {
            if (frame.getStack(stackIndex).insns.any(predicate)) return true
        }
        return false
    }

    private fun firstConsumedStackIndex(
        invocation: MethodInsnNode,
        frame: org.objectweb.asm.tree.analysis.Frame<SourceValue>,
    ): Int? {
        val argumentCount = Type.getArgumentTypes(invocation.desc).size
        val receiverCount = if (invocation.opcode == Opcodes.INVOKESTATIC) 0 else 1
        val first = frame.stackSize - argumentCount - receiverCount
        return first.takeIf { it >= 0 }
    }

    private fun argumentSources(
        invocation: MethodInsnNode,
        frame: org.objectweb.asm.tree.analysis.Frame<SourceValue>?,
        argumentFromEnd: Int,
    ): Set<AbstractInsnNode> {
        if (frame == null) return emptySet()
        val argumentCount = Type.getArgumentTypes(invocation.desc).size
        val stackIndex = frame.stackSize - 1 - argumentFromEnd
        if (argumentCount <= argumentFromEnd || stackIndex !in 0 until frame.stackSize) return emptySet()
        return frame.getStack(stackIndex).insns
    }

    private fun argumentHasSource(
        invocation: MethodInsnNode,
        frame: org.objectweb.asm.tree.analysis.Frame<SourceValue>?,
        argumentFromEnd: Int,
        predicate: (AbstractInsnNode) -> Boolean,
    ): Boolean {
        if (frame == null) return false
        val argumentCount = Type.getArgumentTypes(invocation.desc).size
        val stackIndex = frame.stackSize - 1 - argumentFromEnd
        return argumentCount > argumentFromEnd && stackIndex in 0 until frame.stackSize &&
            frame.getStack(stackIndex).insns.any(predicate)
    }

    private fun valueSources(frame: org.objectweb.asm.tree.analysis.Frame<SourceValue>?): Set<InvokeDynamicInsnNode> {
        if (frame == null || frame.stackSize == 0) return emptySet()
        return frame.getStack(frame.stackSize - 1).insns.filterIsInstanceTo(linkedSetOf())
    }

    internal fun analyzeClassBased(
        node: ClassNode,
        classSuppression: LambdaCaptureSuppression?,
    ): LambdaCapture? {
        if (!isClassBasedLambda(node.name, node.access, node.superName, hasFunctionalInterface(node.interfaces))) {
            return null
        }
        val fields = ArrayList<LambdaCapturedValue>()
        for (field in node.fields.orEmpty()) {
            if (field.access and Opcodes.ACC_STATIC == 0) {
                capturedField(field.name, field.desc)?.let(fields::add)
            }
        }
        return analyzeClassBased(
            className = node.name,
            superName = node.superName,
            interfaces = node.interfaces.orEmpty(),
            outerClass = node.outerClass,
            outerMethod = node.outerMethod,
            outerMethodDescriptor = node.outerMethodDesc,
            fields = fields,
            classSuppression = classSuppression,
        )
    }

    internal fun analyzeClassBased(
        className: String,
        superName: String?,
        interfaces: List<String>,
        outerClass: String?,
        outerMethod: String?,
        outerMethodDescriptor: String?,
        fields: List<LambdaCapturedValue>,
        classSuppression: LambdaCaptureSuppression?,
    ): LambdaCapture? {
        if (fields.isEmpty()) return null
        val coroutine = isCoroutine(superName, interfaces)
        val representation = if (coroutine) "coroutine" else "class"
        val enclosingClass = outerClass?.replace('/', '.')
        val owner = if (enclosingClass != null && outerMethod != null) {
            "$enclosingClass.$outerMethod"
        } else {
            className.replace('/', '.')
        }
        val functionalInterface = interfaces.firstOrNull(::isFunctionalInterface) ?: superName.orEmpty()
        return LambdaCapture(
            callsiteId = OwnerIds.canonical(
                OwnerIds.methodId(className, outerMethod ?: "<lambda>", outerMethodDescriptor ?: superName.orEmpty()),
            ),
            owner = owner,
            implementation = className.replace('/', '.'),
            functionalInterface = functionalInterface.replace('/', '.'),
            representation = representation,
            values = fields,
            sinks = emptyList(),
            desugared = isDesugared(className),
            suppressed = classSuppression != null,
            suppressionReason = classSuppression?.reason,
        )
    }

    internal fun capturedField(name: String, descriptor: String): LambdaCapturedValue? {
        val type = Type.getType(descriptor)
        val role = when {
            name.matches(SPILL_FIELD) -> "spill:$name"
            isCaptureFieldName(name) -> "capture:$name"
            else -> return null
        }
        return LambdaCapturedValue(readableType(type), role, captureStrength(type))
    }

    private fun hasWeakForceUnwrap(
        instructions: Array<out AbstractInsnNode>,
        frames: Array<out org.objectweb.asm.tree.analysis.Frame<SourceValue>?>,
    ): Boolean {
        for (index in instructions.indices) {
            val call = instructions[index] as? MethodInsnNode ?: continue
            if (
                LambdaSinkCatalog.isForceUnwrap(call) &&
                consumedHasSource(call, frames[index]) { source ->
                    source is MethodInsnNode && isWeakGet(source)
                }
            ) {
                return true
            }
        }
        return false
    }

    internal fun applyWeakForceUnwrap(
        captures: MutableList<LambdaCapture>,
        forceUnwrapMethods: Set<String>,
    ) {
        if (forceUnwrapMethods.isEmpty()) return
        for (index in captures.indices) {
            val capture = captures[index]
            if (
                capture.values.any { it.strength == "weak" } &&
                (capture.weakImplementationKey in forceUnwrapMethods ||
                    capture.representation != "invokedynamic" && forceUnwrapMethods.isNotEmpty())
            ) {
                captures[index] = capture.copy(weakDereference = "force_unwrap")
            }
        }
    }

    private fun isWeakGet(call: MethodInsnNode): Boolean {
        return call.owner == WEAK_REFERENCE && call.name == "get" && call.desc == "()Ljava/lang/Object;"
    }

    internal fun suppression(
        visible: List<AnnotationNode>?,
        invisible: List<AnnotationNode>?,
    ): LambdaCaptureSuppression? {
        val annotations = visible.orEmpty() + invisible.orEmpty()
        if (annotations.any { it.desc == IGNORE_DESCRIPTOR }) {
            return LambdaCaptureSuppression("JankHunterIgnore")
        }
        val annotation = annotations.firstOrNull { it.desc == SUPPRESS_DESCRIPTOR } ?: return null
        val values = annotation.values.orEmpty()
        for (index in values.indices step 2) {
            if (values.getOrNull(index) == "reason") {
                return LambdaCaptureSuppression(normalizeSuppressionReason(values.getOrNull(index + 1) as? String))
            }
        }
        return LambdaCaptureSuppression(DEFAULT_SUPPRESSION_REASON)
    }

    internal fun isClassBasedLambda(
        name: String,
        access: Int,
        superName: String?,
        hasFunctionalInterface: Boolean,
    ): Boolean {
        if (isCoroutine(superName, hasFunctionalInterface)) return true
        if (hasFunctionalInterface && access and Opcodes.ACC_SYNTHETIC != 0) return true
        return isDesugared(name) || "\$lambda\$" in name
    }

    private fun isCoroutine(superName: String?, interfaces: List<String>): Boolean {
        return isCoroutine(superName, interfaces.any { it.startsWith(KOTLIN_FUNCTION_PREFIX) })
    }

    private fun isCoroutine(superName: String?, hasFunctionalInterface: Boolean): Boolean {
        return superName in COROUTINE_BASES || hasFunctionalInterface &&
            superName?.endsWith("SuspendLambda") == true
    }

    private fun isFunctionalInterface(name: String): Boolean {
        return name.startsWith(KOTLIN_FUNCTION_PREFIX) || name in JVM_FUNCTIONAL_INTERFACES
    }

    internal fun hasFunctionalInterface(interfaces: Iterable<String>?): Boolean {
        return interfaces?.any(::isFunctionalInterface) == true
    }

    internal fun hasFunctionalInterface(interfaces: Array<out String>?): Boolean {
        return interfaces?.any(::isFunctionalInterface) == true
    }

    private fun isDesugared(name: String): Boolean {
        return "\$\$ExternalSyntheticLambda" in name || "\$r8\$lambda\$" in name ||
            DESUGAR_LAMBDA_SUFFIX.containsMatchIn(name)
    }

    private fun isCaptureFieldName(name: String): Boolean {
        return name == "this\$0" || name == "\$receiver" || name.startsWith("arg\$") ||
            name.startsWith("f\$") || name.startsWith("\$")
    }

    private fun readableType(type: Type): String = type.className

    private fun captureStrength(type: Type): String {
        return when {
            type.sort != Type.OBJECT && type.sort != Type.ARRAY -> "value"
            type.internalName == WEAK_REFERENCE || type.internalName?.startsWith("java/lang/ref/WeakReference") == true -> "weak"
            else -> "strong"
        }
    }

    private fun isLambdaMetafactoryCall(instruction: AbstractInsnNode): Boolean {
        val dynamic = instruction as? InvokeDynamicInsnNode ?: return false
        return dynamic.bsm.owner == LAMBDA_METAFACTORY && dynamic.bsm.name in LAMBDA_BOOTSTRAP_METHODS
    }

    internal data class LambdaCaptureSuppression(val reason: String)

    internal data class MethodAnalysis(
        val captures: List<LambdaCapture>,
        val forceUnwrapImplementationKey: String?,
    ) {
        companion object {
            val EMPTY = MethodAnalysis(emptyList(), null)
        }
    }

    private class MutableLambdaCapture(
        val callsiteId: String,
        val owner: String,
        val implementation: String,
        val functionalInterface: String,
        val values: List<LambdaCapturedValue>,
        val line: Int?,
        val weakImplementationKey: String?,
        val suppression: LambdaCaptureSuppression?,
    ) {
        private var sinks: ArrayList<String>? = null

        fun addSink(sink: String) {
            val current = sinks ?: ArrayList<String>(2).also { sinks = it }
            if (sink !in current) current += sink
        }

        fun finish(): LambdaCapture {
            val orderedSinks = sinks?.apply { sort() }.orEmpty()
            return LambdaCapture(
                callsiteId = callsiteId,
                owner = owner,
                implementation = implementation,
                functionalInterface = functionalInterface,
                representation = "invokedynamic",
                values = values,
                sinks = orderedSinks,
                line = line,
                suppressed = suppression != null,
                suppressionReason = suppression?.reason,
                weakImplementationKey = weakImplementationKey,
            )
        }
    }

    private class LambdaSourceInterpreter : SourceInterpreter(Opcodes.ASM9) {
        override fun copyOperation(instruction: AbstractInsnNode, value: SourceValue): SourceValue = value

        override fun unaryOperation(instruction: AbstractInsnNode, value: SourceValue): SourceValue? {
            return if (instruction is TypeInsnNode && instruction.opcode == Opcodes.CHECKCAST) {
                value
            } else {
                super.unaryOperation(instruction, value)
            }
        }

        override fun naryOperation(
            instruction: AbstractInsnNode,
            values: MutableList<out SourceValue>,
        ): SourceValue? {
            val result = super.naryOperation(instruction, values) ?: return null
            val call = instruction as? MethodInsnNode ?: return result
            if (!LambdaSinkCatalog.isFlowPipeline(call)) return result
            val sources = linkedSetOf<AbstractInsnNode>()
            for (value in values) {
                for (source in value.insns) {
                    if (source is InvokeDynamicInsnNode) sources += source
                    if (sources.size > MAX_FLOW_LAMBDA_SOURCES) return result
                }
            }
            return if (sources.isEmpty()) result else SourceValue(result.size, sources)
        }
    }

    private const val LAMBDA_METAFACTORY = "java/lang/invoke/LambdaMetafactory"
    private const val KOTLIN_FUNCTION_PREFIX = "kotlin/jvm/functions/Function"
    private const val WEAK_REFERENCE = "java/lang/ref/WeakReference"
    private const val SUPPRESS_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterSuppress;"
    private const val IGNORE_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterIgnore;"
    private const val MAX_FLOW_LAMBDA_SOURCES = 64
    private const val DEFAULT_SUPPRESSION_REASON = "reason not provided"
    private val LAMBDA_BOOTSTRAP_METHODS = setOf("metafactory", "altMetafactory")
    private val DESUGAR_LAMBDA_SUFFIX = Regex("\\\$Lambda\\\$\\d+$")
    private val SPILL_FIELD = Regex("[LIJFDZBCS]\\$\\d+")
    private val COROUTINE_BASES = setOf(
        "kotlin/coroutines/jvm/internal/BaseContinuationImpl",
        "kotlin/coroutines/jvm/internal/ContinuationImpl",
        "kotlin/coroutines/jvm/internal/SuspendLambda",
    )
    private val JVM_FUNCTIONAL_INTERFACES = setOf(
        "java/lang/Runnable",
        "java/util/concurrent/Callable",
        "java/util/function/Consumer",
        "java/util/function/Function",
        "java/util/function/Predicate",
        "java/util/function/Supplier",
    )

    internal fun implementationKey(owner: String, name: String, descriptor: String): String {
        return "$owner#$name$descriptor"
    }

    internal fun normalizeSuppressionReason(reason: String?): String {
        return reason?.trim().takeUnless(String?::isNullOrEmpty) ?: DEFAULT_SUPPRESSION_REASON
    }
}

internal inline fun <T> bestEffortAsmAnalysis(block: () -> T): T? {
    return try {
        block()
    } catch (exception: Exception) {
        if (exception is InterruptedException || exception is java.util.concurrent.CancellationException) {
            throw exception
        }
        null
    }
}

private object LambdaSinkCatalog {
    fun mayConsumeLambda(instruction: AbstractInsnNode): Boolean {
        return when (instruction) {
            is MethodInsnNode -> sink(instruction) != null || isFlowLaunchIn(instruction) || isCoroutineBuilder(instruction)
            is FieldInsnNode -> instruction.opcode == Opcodes.PUTSTATIC || instruction.opcode == Opcodes.PUTFIELD
            else -> false
        }
    }

    fun sink(call: MethodInsnNode): String? {
        return when {
            call.owner == "androidx/compose/runtime/Composer" &&
                call.name in COMPOSER_RETAIN_METHODS -> "compose.remember"
            call.owner.startsWith("androidx/compose/runtime/") && call.name == "derivedStateOf" -> "compose.derived_state"
            call.owner.startsWith("androidx/compose/runtime/") && call.name == "CompositionLocalProvider" -> "compose.provider"
            call.owner.startsWith("kotlinx/coroutines/flow/") && call.name in FLOW_OPERATORS -> "flow.operator"
            call.owner.startsWith("kotlinx/coroutines/flow/") && call.name == "callbackFlow" -> "flow.callback"
            call.name == "repeatOnLifecycle" && call.owner.startsWith("androidx/lifecycle/") -> "flow.repeat_on_lifecycle"
            call.name.startsWith("launchWhen") && call.owner.startsWith("androidx/lifecycle/") -> "flow.lifecycle_launch"
            isCoroutineBuilder(call) -> "coroutine.builder"
            (call.owner == "android/os/Handler" || call.owner.startsWith("android/view/")) &&
                (call.name.startsWith("post") || call.name == "sendMessage") -> "handler.queue"
            isExecutor(call.owner) && call.name in EXECUTOR_METHODS -> "executor.queue"
            call.name.startsWith("setOn") && call.name.endsWith("Listener") -> "listener.registration"
            call.name in LISTENER_METHODS -> "listener.registration"
            else -> null
        }
    }

    fun isFlowLaunchIn(call: MethodInsnNode): Boolean {
        return call.owner.startsWith("kotlinx/coroutines/flow/") && call.name == "launchIn"
    }

    fun isFlowPipeline(call: MethodInsnNode): Boolean {
        return call.owner.startsWith("kotlinx/coroutines/flow/") && Type.getReturnType(call.desc).sort != Type.VOID
    }

    fun isCoroutineBuilder(call: MethodInsnNode): Boolean {
        return call.owner.startsWith("kotlinx/coroutines/BuildersKt") && call.name in COROUTINE_BUILDERS
    }

    fun isGlobalScopeSource(instruction: AbstractInsnNode): Boolean {
        return instruction is FieldInsnNode && instruction.opcode == Opcodes.GETSTATIC &&
            instruction.owner == "kotlinx/coroutines/GlobalScope"
    }

    fun isLifecycleScopeSource(instruction: AbstractInsnNode): Boolean {
        return instruction is MethodInsnNode && instruction.owner.startsWith("androidx/lifecycle/") &&
            instruction.name in LIFECYCLE_SCOPE_GETTERS
    }

    fun isForceUnwrap(call: MethodInsnNode): Boolean {
        return (call.owner == "kotlin/jvm/internal/Intrinsics" && call.name.startsWith("checkNotNull")) ||
            (call.owner == "java/util/Objects" && call.name == "requireNonNull")
    }

    private fun isExecutor(owner: String): Boolean {
        return owner == "java/util/concurrent/Executor" || owner == "java/util/concurrent/ExecutorService" ||
            owner.endsWith("Executor") || owner.endsWith("ExecutorService")
    }

    private val FLOW_OPERATORS = setOf("collect", "collectLatest", "map", "filter", "onEach", "transform")
    // Composer.cache executes its initializer immediately; only updateRememberedValue retains its argument.
    private val COMPOSER_RETAIN_METHODS = setOf("updateRememberedValue")
    private val COROUTINE_BUILDERS = setOf("launch", "async", "withContext")
    private val EXECUTOR_METHODS = setOf("execute", "submit", "schedule", "scheduleAtFixedRate", "scheduleWithFixedDelay")
    private val LISTENER_METHODS = setOf("addListener", "addObserver", "registerCallback", "registerReceiver")
    private val LIFECYCLE_SCOPE_GETTERS = setOf("getLifecycleScope", "getViewModelScope")
}
