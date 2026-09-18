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
                    selection.runtime -> hookConfig.copy(lifecycleLeaks = false)
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
    private val syntheticLifecycleCallbacks: Map<String, Int>? = null,
    private val existingLifecycleMethods: Set<String> = emptySet(),
) : ClassVisitor(Opcodes.ASM9, next) {
    private val edges = linkedMapOf<ClassGraphEdgeKey, Int>()
    private val lambdaCaptures = if (config.classGraph) LambdaCaptureClassBuilder(className) else null
    private val classAnnotations = JankAnnotationMetadata.Builder()
    private val diagnostics = InstrumentationDiagnosticsClassBuilder(
        className,
        if (instrumentationMarkerDescriptor == LifecycleInstrumentationMarker.DESCRIPTOR) "lifecycle" else "main",
    )
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
        lambdaCaptures?.recordField(access, name, descriptor)
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
                config.coroutines,
                name,
                descriptor,
                classHierarchy,
            )
        ) {
            OwnerIds.coroutineOwner(className, enclosingOwner, enclosingMethod)
        } else {
            null
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
                coroutineOwner = coroutineOwner,
                recordBinderHookApplied = {
                    androidComponentCatalog.recordInstrumented(name, descriptor)
                    classHookApplied = true
                },
            )
        }
        if (!config.databaseTracing) {
            val captures = lambdaCaptures
            if (captures == null) return instrument(next, emptyList()) {}
            return object : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
                override fun visitEnd() {
                    super.visitEnd()
                    captures.recordMethod(this)
                    accept(instrument(next, emptyList()) {})
                }
            }
        }
        return object : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
            override fun visitEnd() {
                super.visitEnd()
                lambdaCaptures?.recordMethod(this)
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
        emitSyntheticLifecycleMethodsIfNeeded()
        emitSyntheticAutoInitMethodIfNeeded()
        if (!alreadyInstrumented && (!markerOnlyWhenHookApplied || classHookApplied)) {
            super.visitAnnotation(instrumentationMarkerDescriptor, false)?.visitEnd()
        }
        if (config.classGraph) {
            ClassGraphWriter.write(config.classGraphDirectory, className, edges)
            LambdaCaptureWriter.write(
                config.lambdaCaptureDirectory,
                className,
                lambdaCaptures?.finish().orEmpty(),
            )
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
        val captures = lambdaCaptures ?: return target
        return object : MethodNode(Opcodes.ASM9, access, name, descriptor, signature, exceptions) {
            override fun visitEnd() {
                super.visitEnd()
                captures.recordMethod(this)
                accept(target)
            }
        }
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
