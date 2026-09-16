package io.jankhunter.gradle

import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.MethodNode

internal class LambdaCaptureClassBuilder(
    private val className: String,
) {
    private var actualClassName = className
    private var superName: String? = null
    private var interfaces: List<String> = emptyList()
    private var outerClass: String? = null
    private var outerMethod: String? = null
    private var outerMethodDescriptor: String? = null
    private var classBasedCandidate = false
    private var capturedFields: ArrayList<LambdaCapturedValue>? = null
    private var captures: ArrayList<LambdaCapture>? = null
    private var forceUnwrapMethods: HashSet<String>? = null
    private var classSuppression: LambdaCaptureAnalyzer.LambdaCaptureSuppression? = null

    fun recordClass(
        access: Int,
        name: String?,
        superName: String?,
        interfaces: Array<out String>?,
    ) {
        actualClassName = name ?: className
        this.superName = superName
        classBasedCandidate = LambdaCaptureAnalyzer.isClassBasedLambda(
            actualClassName,
            access,
            superName,
            LambdaCaptureAnalyzer.hasFunctionalInterface(interfaces),
        )
        if (classBasedCandidate && !interfaces.isNullOrEmpty()) {
            this.interfaces = interfaces.toList()
        }
    }

    fun recordOuterClass(owner: String?, name: String?, descriptor: String?) {
        if (!classBasedCandidate) return
        outerClass = owner
        outerMethod = name
        outerMethodDescriptor = descriptor
    }

    fun annotationVisitor(descriptor: String, delegate: AnnotationVisitor?): AnnotationVisitor? {
        return when (descriptor) {
            IGNORE_DESCRIPTOR -> {
                classSuppression = LambdaCaptureAnalyzer.LambdaCaptureSuppression("JankHunterIgnore")
                delegate
            }
            SUPPRESS_DESCRIPTOR -> object : AnnotationVisitor(Opcodes.ASM9, delegate) {
                override fun visit(name: String?, value: Any?) {
                    if (name == "reason" && value is String) {
                        classSuppression = LambdaCaptureAnalyzer.LambdaCaptureSuppression(
                            LambdaCaptureAnalyzer.normalizeSuppressionReason(value),
                        )
                    }
                    super.visit(name, value)
                }

                override fun visitEnd() {
                    if (classSuppression == null) {
                        classSuppression = LambdaCaptureAnalyzer.LambdaCaptureSuppression(
                            LambdaCaptureAnalyzer.normalizeSuppressionReason(null),
                        )
                    }
                    super.visitEnd()
                }
            }
            else -> delegate
        }
    }

    fun recordField(
        access: Int,
        name: String?,
        descriptor: String,
    ) {
        if (!classBasedCandidate || name == null || access and Opcodes.ACC_STATIC != 0) return
        val captured = LambdaCaptureAnalyzer.capturedField(name, descriptor) ?: return
        val fields = capturedFields ?: ArrayList<LambdaCapturedValue>().also { capturedFields = it }
        fields += captured
    }

    fun recordMethod(method: MethodNode) {
        val analysis = LambdaCaptureAnalyzer.inspectMethod(actualClassName, method, classSuppression)
        if (analysis.captures.isNotEmpty()) {
            val current = captures ?: ArrayList<LambdaCapture>().also { captures = it }
            current += analysis.captures
        }
        analysis.forceUnwrapImplementationKey?.let { key ->
            val current = forceUnwrapMethods ?: HashSet<String>().also { forceUnwrapMethods = it }
            current += key
        }
    }

    fun finish(): List<LambdaCapture> {
        if (classBasedCandidate) {
            LambdaCaptureAnalyzer.analyzeClassBased(
                className = actualClassName,
                superName = superName,
                interfaces = interfaces,
                outerClass = outerClass,
                outerMethod = outerMethod,
                outerMethodDescriptor = outerMethodDescriptor,
                fields = capturedFields.orEmpty(),
                classSuppression = classSuppression,
            )?.let { capture ->
                val current = captures ?: ArrayList<LambdaCapture>().also { captures = it }
                current += capture
            }
        }
        val result = captures ?: return emptyList()
        LambdaCaptureAnalyzer.applyWeakForceUnwrap(result, forceUnwrapMethods.orEmpty())
        result.sortBy(LambdaCapture::callsiteId)
        return result
    }

    private companion object {
        const val SUPPRESS_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterSuppress;"
        const val IGNORE_DESCRIPTOR = "Lio/jankhunter/annotations/JankHunterIgnore;"
    }
}

internal object LambdaCaptureWriter {
    fun write(directoryPath: String, className: String, captures: List<LambdaCapture>) {
        if (captures.isEmpty()) {
            InstrumentationArtifactFiles.writeClassShard(directoryPath, className, "")
            return
        }
        val dottedClassName = className.replace('/', '.')
        InstrumentationArtifactFiles.writeClassShard(directoryPath, className) { writer ->
            var start = 0
            while (start < captures.size) {
                val end = (start + LAMBDA_CAPTURES_PER_RECORD).coerceAtMost(captures.size)
                writer.write(record(dottedClassName, captures, start, end))
                start = end
            }
        }
    }

    private fun record(
        className: String,
        captures: List<LambdaCapture>,
        start: Int,
        end: Int,
    ): String {
        return buildString((end - start) * LAMBDA_CAPTURE_ESTIMATED_BYTES) {
            append("{\"format\":")
            append(ArtifactSchemas.LAMBDA_CAPTURE_FORMAT)
            append(",\"class\":\"")
            append(escapeJsonString(className))
            append("\",\"captures\":[")
            for (index in start until end) {
                if (index > start) append(',')
                appendCapture(captures[index])
            }
            append("]}\n")
        }
    }

    private fun StringBuilder.appendCapture(capture: LambdaCapture) {
        append("{\"callsiteId\":\"")
        append(escapeJsonString(capture.callsiteId))
        append("\",\"owner\":\"")
        append(escapeJsonString(capture.owner))
        append("\",\"implementation\":\"")
        append(escapeJsonString(capture.implementation))
        append("\",\"functionalInterface\":\"")
        append(escapeJsonString(capture.functionalInterface))
        append("\",\"representation\":\"")
        append(escapeJsonString(capture.representation))
        append("\",\"values\":[")
        capture.values.forEachIndexed { index, value ->
            if (index > 0) append(',')
            append("{\"type\":\"")
            append(escapeJsonString(value.type))
            append("\",\"role\":\"")
            append(escapeJsonString(value.role))
            append("\",\"strength\":\"")
            append(escapeJsonString(value.strength))
            append("\"}")
        }
        append("],\"sinks\":[")
        capture.sinks.forEachIndexed { index, sink ->
            if (index > 0) append(',')
            append('"')
            append(escapeJsonString(sink))
            append('"')
        }
        append(']')
        capture.line?.let {
            append(",\"line\":")
            append(it)
        }
        if (capture.desugared) append(",\"desugared\":true")
        capture.weakDereference?.let {
            append(",\"weakDereference\":\"")
            append(escapeJsonString(it))
            append('"')
        }
        if (capture.suppressed) {
            append(",\"suppressed\":true,\"suppressionReason\":\"")
            append(escapeJsonString(LambdaCaptureAnalyzer.normalizeSuppressionReason(capture.suppressionReason)))
            append('"')
        }
        append('}')
    }
}

internal const val LAMBDA_CAPTURES_PER_RECORD = 256
private const val LAMBDA_CAPTURE_ESTIMATED_BYTES = 384
