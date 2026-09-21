package io.jankhunter.gradle

import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes
import org.objectweb.asm.tree.ClassNode
import org.objectweb.asm.tree.MethodInsnNode

/** One indexed program, streamed one class at a time. All names here precede R8. */
internal class LifecycleScopedTransformer(
    private val index: LifecycleClassIndex,
    private val selected: Set<String>,
    private val diagnosticsDirectory: String,
    private val canAddAncestorAccessor: (LifecycleClassHeader) -> Boolean = { false },
    private val diagnostic: (String) -> Unit,
) {
    private val plans = index.newWorkMap<Int>()
    private val accessorOwners = index.newWorkMap<Boolean>()
    private val fragmentOwners = index.newWorkMap<Boolean>()
    private val callbackOwners = index.newWorkMap<Boolean>()

    init {
        val planner = LifecycleCallbackPlanner(index)
        for (owner in selected) {
            index.maintainBudget()
            val kind = index.kind(owner)
            if (kind == LifecycleTargetKind.NONE) continue
            val explicitBindingAccessor = index.hasExplicitBindingAccessor(owner)
            for (ancestor in index.superclasses(owner)) {
                // Framework implementations own internal references (for example DialogFragment).
                // Their public getView contract supplies the root; only application fields need accessors.
                if (LifecycleTargetKind.fromRoot(ancestor) != null || ancestor.startsWith("android/") ||
                    ancestor.startsWith("androidx/")) break
                val header = index.header(ancestor) ?: error("Missing lifecycle hierarchy metadata: $ancestor")
                validateAccessorHeader(header)
                if (ancestor !in selected || !header.programClass) {
                    if (kind == LifecycleTargetKind.FRAGMENT && header.hasReferenceFields &&
                        ACCESSOR_MARKER !in header.annotations && !explicitBindingAccessor && canAddAncestorAccessor(header)) {
                        accessorOwners[ancestor] = true
                        fragmentOwners[ancestor] = true
                        continue
                    }
                    check(kind != LifecycleTargetKind.FRAGMENT || !header.hasReferenceFields ||
                        ACCESSOR_MARKER in header.annotations || explicitBindingAccessor) {
                        "Cannot fully capture $owner: excluded ancestor $ancestor has reference fields. " +
                            "Change the include/exclude selection or @JankHunterIgnore, or implement " +
                            "JankHunterBindingAccessor to expose its existing bindings explicitly. " +
                            "The excluded ancestor will not be modified."
                    }
                    continue
                }
                accessorOwners[ancestor] = true
                if (kind == LifecycleTargetKind.FRAGMENT) fragmentOwners[ancestor] = true
            }
            for (callback in kind.callbacks) {
                when (val plan = planner.plan(owner, callback)) {
                    is LifecycleCallbackPlan.InstrumentDeclaration -> {
                        check(plan.owner in selected) {
                            "Cannot safely intercept $owner.$callback: excluded declaration ${plan.owner}.$callback " +
                                "requires a hook in excluded code. Change the include/exclude selection or " +
                                "@JankHunterIgnore; a binding accessor cannot intercept a final lifecycle callback."
                        }
                        callbackOwners[plan.owner] = true
                    }
                    is LifecycleCallbackPlan.Override -> {
                        plans[owner + "\u0000" + callback] = plan.access
                        callbackOwners[owner] = true
                    }
                    LifecycleCallbackPlan.AbstractDeclaration -> Unit
                    is LifecycleCallbackPlan.Unsupported -> error(plan.reason)
                }
            }
        }
    }

    fun transform(bytes: ByteArray): ByteArray {
        val reader = ClassReader(bytes)
        if (reader.className !in accessorOwners && reader.className !in callbackOwners) return bytes
        val node = ClassNode()
        reader.accept(node, ClassReader.EXPAND_FRAMES)
        val accessorPresent = ACCESSOR_MARKER in node.invisibleAnnotations.orEmpty().map { it.desc }
        if (accessorPresent) {
            check(LifecycleAccessorEmitter.ACCESSOR in node.interfaces &&
                node.methods.any { it.name == LifecycleAccessorEmitter.KIND_METHOD && it.desc == "()I" } &&
                node.methods.any { it.name == LifecycleAccessorEmitter.VISIT_METHOD && it.desc == LifecycleAccessorEmitter.VISIT_DESCRIPTOR }) {
                "Invalid versioned lifecycle accessor in ${node.name}"
            }
            return bytes
        }
        val existingHooks = node.methods.filter { method ->
            method.instructions.asSequence().filterIsInstance<MethodInsnNode>().any {
                it.owner == "io/jankhunter/runtime/JankHunterHooks" && it.name == "watchLifecycleObject"
            }
        }.mapTo(hashSetOf()) { it.name + it.desc }
        // Old callbacks keep their ABI and become typed through the generated interface.
        // Remove only the pass marker so missing inherited callbacks can still be added.
        node.invisibleAnnotations?.removeAll { it.desc == LifecycleInstrumentationMarker.DESCRIPTOR }
        val frameTypes = LifecycleFrameTypes(index)
        val writer = object : ClassWriter(COMPUTE_FRAMES or COMPUTE_MAXS) {
            override fun getCommonSuperClass(type1: String, type2: String): String = frameTypes.common(type1, type2)
        }
        if (node.name in callbackOwners) {
            node.accept(JankHunterClassVisitor(
                next = writer,
                className = node.name.replace('/', '.'),
                config = lifecycleHookConfig(diagnosticsDirectory),
                classHierarchy = index.superclasses(node.name).toSet(),
                instrumentationMarkerDescriptor = LifecycleInstrumentationMarker.DESCRIPTOR,
                markerOnlyWhenHookApplied = true,
                diagnosticsOnlyWhenHookApplied = true,
                syntheticLifecycleCallbacks = index.kind(node.name).callbacks.mapNotNull { callback ->
                    plans[node.name + "\u0000" + callback]?.let { callback to it }
                }.toMap(),
                existingLifecycleMethods = existingHooks,
            ))
        } else {
            node.accept(writer)
        }
        val result = ClassNode()
        ClassReader(writer.toByteArray()).accept(result, 0)
        if (node.name in accessorOwners) {
            val kind = if (node.name in selected) index.kind(node.name) else LifecycleTargetKind.NONE
            val parentAccessor = index.superclasses(node.name).drop(1).any {
                it in accessorOwners || index.header(it)?.accessorMethods?.containsKey(
                    LifecycleAccessorEmitter.VISIT_METHOD + LifecycleAccessorEmitter.VISIT_DESCRIPTOR,
                ) == true
            }
            val fragment = node.name in fragmentOwners
            val root = if (fragment && !parentAccessor) index.superclasses(node.name).firstOrNull {
                LifecycleTargetKind.fromRoot(it) == LifecycleTargetKind.FRAGMENT
            } else null
            LifecycleAccessorEmitter(
                result, kind, parentAccessor,
                index.header(LifecycleAccessorEmitter.BINDING) != null,
                index.header("kotlin/Lazy") != null,
                captureFragmentFields = fragment,
                fragmentViewOwner = root,
            ).emit()
            result.visitAnnotation(ACCESSOR_MARKER, false).visitEnd()
            if (fragment) diagnoseCustomDelegates(node)
        }
        return ClassWriter(0).also { result.accept(it) }.toByteArray()
    }

    private fun diagnoseCustomDelegates(node: ClassNode) {
        var metadata = KotlinGeneratedMethodIndex.EMPTY
        (node.visibleAnnotations.orEmpty() + node.invisibleAnnotations.orEmpty())
            .firstOrNull { it.desc == KotlinGeneratedMethodIndex.METADATA_DESCRIPTOR }
            ?.accept(KotlinGeneratedMethodIndex.collectingVisitor(null) { metadata = it })
        val properties = metadata.delegatedProperties.filterValues { it != "Lkotlin/Lazy;" }.keys.toMutableSet()
        node.fields.filter {
            it.access and Opcodes.ACC_STATIC == 0 && it.name.endsWith("\$delegate") && it.desc != "Lkotlin/Lazy;"
        }.forEach { properties += it.name.removeSuffix("\$delegate") }
        properties.sorted().forEach {
            diagnostic("${node.name}.$it: custom delegate is not invoked; implement JankHunterBindingAccessor to expose existing bindings")
        }
    }

    private fun validateAccessorHeader(header: LifecycleClassHeader) {
        val marked = ACCESSOR_MARKER in header.annotations
        if (header.accessorMethods.isEmpty() && !marked) return
        check(marked && LifecycleAccessorEmitter.ACCESSOR in header.interfaces) {
            "Lifecycle accessor name collision in ${header.name}"
        }
        for (signature in listOf(
            LifecycleAccessorEmitter.KIND_METHOD + "()I",
            LifecycleAccessorEmitter.VISIT_METHOD + LifecycleAccessorEmitter.VISIT_DESCRIPTOR,
        )) {
            val flags = header.accessorMethods[signature]
            check(flags != null && flags and Opcodes.ACC_PUBLIC != 0 &&
                flags and (Opcodes.ACC_FINAL or Opcodes.ACC_STATIC or Opcodes.ACC_ABSTRACT or Opcodes.ACC_NATIVE) == 0) {
                "Invalid lifecycle accessor ABI in ${header.name}: $signature"
            }
        }
    }

    companion object {
        const val ACCESSOR_MARKER = "Lio/jankhunter/runtime/JankHunterLifecycleAccessorsV1;"
    }
}
