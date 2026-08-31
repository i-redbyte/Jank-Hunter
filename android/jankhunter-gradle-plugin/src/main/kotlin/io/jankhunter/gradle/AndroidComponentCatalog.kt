package io.jankhunter.gradle

import org.objectweb.asm.Opcodes

internal enum class AndroidComponentCatalogKind(val wireName: String) {
    SERVICE("service"),
    RECEIVER("receiver"),
    AIDL_INTERFACE("aidl_interface"),
    AIDL_STUB("aidl_stub"),
    AIDL_PROXY("aidl_proxy"),
    BINDER("binder"),
}

internal enum class AndroidComponentCoverage(val wireName: String) {
    NONE("none"),
    PARTIAL("partial"),
    FULL("full"),
}

internal data class AndroidComponentCatalogRecord(
    val className: String,
    val componentId: Long,
    val kind: AndroidComponentCatalogKind,
    val abstract: Boolean,
    val entryPoints: List<String>,
    val instrumentedEntryPoints: List<String>,
    val uncoveredEntryPoints: List<String>,
    val coverage: AndroidComponentCoverage,
    val aidlDescriptor: String?,
    val transactions: Map<Long, String>,
)

/** Collects only static class metadata; runtime Intent/Parcel contents never cross this boundary. */
internal class AndroidComponentCatalogClassBuilder(
    private val className: String,
    hierarchy: Set<String>,
) {
    private val hierarchy = hierarchy.mapTo(linkedSetOf()) { it.replace('.', '/') }
    private val methods = linkedSetOf<MethodSignature>()
    private val instrumented = linkedSetOf<MethodSignature>()
    private val transactions = sortedMapOf<Long, String>()
    private var classAccess = 0
    private var aidlDescriptor: String? = null
    private var hasBinderField = false

    fun recordClass(access: Int) {
        classAccess = access
    }

    fun recordHierarchyType(className: String) {
        hierarchy += className.replace('.', '/')
    }

    fun recordField(access: Int, name: String, descriptor: String, value: Any?) {
        if (descriptor == BINDER_DESCRIPTOR) hasBinderField = true
        if (name == AIDL_DESCRIPTOR_FIELD && descriptor == STRING_DESCRIPTOR && value is String) {
            aidlDescriptor = value
        }
        if (access and Opcodes.ACC_STATIC != 0 && name.startsWith(TRANSACTION_PREFIX) && value is Number) {
            val code = value.toLong()
            if (code >= 0L) transactions[code] = name.removePrefix(TRANSACTION_PREFIX)
        }
    }

    fun recordMethod(access: Int, name: String, descriptor: String) {
        if (name == CONSTRUCTOR || name == CLASS_INITIALIZER || access and Opcodes.ACC_STATIC != 0) return
        methods += MethodSignature(name, descriptor, access)
    }

    fun recordInstrumented(name: String, descriptor: String) {
        instrumented += MethodSignature(name, descriptor, 0)
    }

    fun declaredAidlDescriptor(): String? = aidlDescriptor

    fun finish(): AndroidComponentCatalogRecord? {
        val kind = classify() ?: return null
        val entryPoints = methods.asSequence()
            .filter { signature -> isEntryPoint(kind, signature) }
            .map(MethodSignature::wireName)
            .sorted()
            .toList()
        val instrumentedEntryPoints = instrumented.asSequence()
            .map(MethodSignature::wireName)
            .filter(entryPoints::contains)
            .distinct()
            .sorted()
            .toList()
        val uncovered = entryPoints.filterNot(instrumentedEntryPoints::contains)
        val coverage = when {
            instrumentedEntryPoints.isEmpty() -> AndroidComponentCoverage.NONE
            uncovered.isEmpty() -> AndroidComponentCoverage.FULL
            else -> AndroidComponentCoverage.PARTIAL
        }
        return AndroidComponentCatalogRecord(
            className = className.replace('/', '.'),
            componentId = OwnerIds.methodId(className, COMPONENT_ID_METHOD, kind.wireName),
            kind = kind,
            abstract = classAccess and (Opcodes.ACC_ABSTRACT or Opcodes.ACC_INTERFACE) != 0,
            entryPoints = entryPoints,
            instrumentedEntryPoints = instrumentedEntryPoints,
            uncoveredEntryPoints = uncovered,
            coverage = coverage,
            aidlDescriptor = aidlDescriptor,
            transactions = transactions.toMap(),
        )
    }

    private fun classify(): AndroidComponentCatalogKind? = when {
        ANDROID_SERVICE in hierarchy -> AndroidComponentCatalogKind.SERVICE
        ANDROID_RECEIVER in hierarchy -> AndroidComponentCatalogKind.RECEIVER
        className.endsWith(AIDL_PROXY_SUFFIX) || hasBinderField && ANDROID_IINTERFACE in hierarchy -> {
            AndroidComponentCatalogKind.AIDL_PROXY
        }
        className.endsWith(AIDL_STUB_SUFFIX) && ANDROID_BINDER in hierarchy -> AndroidComponentCatalogKind.AIDL_STUB
        classAccess and Opcodes.ACC_INTERFACE != 0 && ANDROID_IINTERFACE in hierarchy -> {
            AndroidComponentCatalogKind.AIDL_INTERFACE
        }
        ANDROID_BINDER in hierarchy -> AndroidComponentCatalogKind.BINDER
        else -> null
    }

    private fun isEntryPoint(kind: AndroidComponentCatalogKind, signature: MethodSignature): Boolean = when (kind) {
        AndroidComponentCatalogKind.SERVICE -> signature.key in SERVICE_CALLBACKS
        AndroidComponentCatalogKind.RECEIVER -> signature.key == RECEIVER_CALLBACK
        AndroidComponentCatalogKind.AIDL_STUB,
        AndroidComponentCatalogKind.BINDER,
        -> signature.key == BINDER_ON_TRANSACT
        AndroidComponentCatalogKind.AIDL_INTERFACE,
        AndroidComponentCatalogKind.AIDL_PROXY,
        -> signature.name != AS_BINDER && signature.access and Opcodes.ACC_PUBLIC != 0
    }

    private data class MethodSignature(
        val name: String,
        val descriptor: String,
        val access: Int,
    ) {
        val key: String = "$name$descriptor"
        val wireName: String = key

        override fun equals(other: Any?): Boolean {
            return other is MethodSignature && name == other.name && descriptor == other.descriptor
        }

        override fun hashCode(): Int = 31 * name.hashCode() + descriptor.hashCode()
    }

    private companion object {
        const val ANDROID_SERVICE = "android/app/Service"
        const val ANDROID_RECEIVER = "android/content/BroadcastReceiver"
        const val ANDROID_BINDER = "android/os/Binder"
        const val ANDROID_IINTERFACE = "android/os/IInterface"
        const val BINDER_DESCRIPTOR = "Landroid/os/IBinder;"
        const val STRING_DESCRIPTOR = "Ljava/lang/String;"
        const val AIDL_DESCRIPTOR_FIELD = "DESCRIPTOR"
        const val TRANSACTION_PREFIX = "TRANSACTION_"
        const val AIDL_STUB_SUFFIX = "\$Stub"
        const val AIDL_PROXY_SUFFIX = "\$Stub\$Proxy"
        const val CONSTRUCTOR = "<init>"
        const val CLASS_INITIALIZER = "<clinit>"
        const val AS_BINDER = "asBinder"
        const val COMPONENT_ID_METHOD = "<android-component>"
        const val RECEIVER_CALLBACK = "onReceive(Landroid/content/Context;Landroid/content/Intent;)V"
        const val BINDER_ON_TRANSACT = "onTransact(ILandroid/os/Parcel;Landroid/os/Parcel;I)Z"
        val SERVICE_CALLBACKS = setOf(
            "onCreate()V",
            "onStartCommand(Landroid/content/Intent;II)I",
            "onBind(Landroid/content/Intent;)Landroid/os/IBinder;",
            "onUnbind(Landroid/content/Intent;)Z",
            "onRebind(Landroid/content/Intent;)V",
            "onTaskRemoved(Landroid/content/Intent;)V",
            "onDestroy()V",
            "onTimeout(I)V",
            "onTimeout(II)V",
        )
    }
}

internal object AndroidComponentCatalogWriter {
    fun write(directoryPath: String, record: AndroidComponentCatalogRecord) {
        write(directoryPath, record.className, record)
    }

    fun write(directoryPath: String, className: String, record: AndroidComponentCatalogRecord?) {
        if (directoryPath.isBlank()) return
        InstrumentationArtifactFiles.writeClassShard(
            directoryPath,
            className,
            record?.let(::toJsonLine).orEmpty(),
        )
    }

    private fun toJsonLine(record: AndroidComponentCatalogRecord): String = buildString(512) {
        append("{\"format\":")
        append(ArtifactSchemas.ANDROID_COMPONENT_CATALOG_FORMAT)
        append(",\"class\":\"")
        append(escapeJsonString(record.className))
        append("\",\"componentId\":\"")
        append(OwnerIds.canonical(record.componentId))
        append("\",\"kind\":\"")
        append(record.kind.wireName)
        append("\",\"abstract\":")
        append(record.abstract)
        append(",\"coverage\":\"")
        append(record.coverage.wireName)
        append("\",\"entryPoints\":")
        appendStrings(record.entryPoints)
        append(",\"instrumentedEntryPoints\":")
        appendStrings(record.instrumentedEntryPoints)
        append(",\"uncoveredEntryPoints\":")
        appendStrings(record.uncoveredEntryPoints)
        record.aidlDescriptor?.let { descriptor ->
            append(",\"aidlDescriptor\":\"")
            append(escapeJsonString(descriptor))
            append('"')
        }
        append(",\"transactions\":[")
        record.transactions.entries.forEachIndexed { index, entry ->
            if (index > 0) append(',')
            append("{\"code\":")
            append(entry.key)
            append(",\"method\":\"")
            append(escapeJsonString(entry.value))
            append("\"}")
        }
        append("]}\n")
    }

    private fun StringBuilder.appendStrings(values: List<String>) {
        append('[')
        values.forEachIndexed { index, value ->
            if (index > 0) append(',')
            append('"')
            append(escapeJsonString(value))
            append('"')
        }
        append(']')
    }
}
