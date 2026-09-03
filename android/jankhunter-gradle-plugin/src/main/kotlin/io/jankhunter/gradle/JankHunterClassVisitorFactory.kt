package io.jankhunter.gradle

import com.android.build.api.instrumentation.AsmClassVisitorFactory
import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.FieldVisitor
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.MethodNode

abstract class JankHunterClassVisitorFactory : AsmClassVisitorFactory<JankHunterInstrumentationParameters> {
    override fun createClassVisitor(
        classContext: ClassContext,
        nextClassVisitor: ClassVisitor,
    ): ClassVisitor {
        val params = parameters.get()
        val classData = classContext.currentClassData
        val selection = InstrumentationClassSelector.evaluate(params, classData)
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
        return InstrumentationClassSelector.evaluate(parameters.get(), classData).any()
    }
}

internal class JankHunterClassVisitor(
    next: ClassVisitor,
    private val className: String,
    private val config: HookConfig,
    classHierarchy: Set<String> = setOf(className),
    private val resolveOwnerHierarchy: (String) -> Set<String> = { setOf(it) },
    private val hierarchyResolutionDiagnostics: () -> Map<ClassHierarchyResolutionFailure, Int> = { emptyMap() },
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
        val hierarchyFailures = hierarchyResolutionDiagnostics()
        diagnostics.recordHierarchyResolutionFailures(hierarchyFailures)
        if (hierarchyFailures.isNotEmpty() || !diagnosticsOnlyWhenHookApplied || classHookApplied) {
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
