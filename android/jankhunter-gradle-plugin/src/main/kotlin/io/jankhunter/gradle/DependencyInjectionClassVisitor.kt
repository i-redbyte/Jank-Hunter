package io.jankhunter.gradle

import org.objectweb.asm.AnnotationVisitor
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.FieldVisitor
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes
import org.objectweb.asm.Type

internal class DependencyInjectionClassVisitor(
    next: ClassVisitor,
    private val className: String,
    private val catalogDirectory: String,
    private val generated: Boolean = false,
    generatedFramework: DependencyInjectionFramework? = null,
) : ClassVisitor(Opcodes.ASM9, next) {
    private val dottedClassName = className.toDottedClassName()
    private val frameworks = linkedSetOf<DependencyInjectionFramework>()
    private val roles = linkedSetOf<String>()
    private val scopes = linkedSetOf<String>()
    private val components = linkedSetOf<String>()
    private val edges = linkedSetOf<DependencyInjectionEdgeRecord>()
    private var koinClassDefinition = false
    private var hiltEntryPoint = false

    init {
        generatedFramework?.let(frameworks::add)
        if (generated) classifyGeneratedRole()
    }

    override fun visit(
        version: Int,
        access: Int,
        name: String?,
        signature: String?,
        superName: String?,
        interfaces: Array<out String>?,
    ) {
        classifyGeneratedHierarchy(superName, interfaces.orEmpty())
        super.visit(version, access, name, signature, superName, interfaces)
    }

    override fun visitAnnotation(descriptor: String, visible: Boolean): AnnotationVisitor? {
        val delegate = super.visitAnnotation(descriptor, visible)
        val annotation = descriptor.annotationClassName()
        when (annotation) {
            DAGGER_MODULE -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_MODULE
            }
            DAGGER_COMPONENT -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_COMPONENT
            }
            DAGGER_SUBCOMPONENT -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_SUBCOMPONENT
            }
            JAVAX_SCOPE,
            JAKARTA_SCOPE -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_SCOPE
            }
            JAVAX_QUALIFIER,
            JAKARTA_QUALIFIER -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_QUALIFIER
            }
            HILT_INSTALL_IN -> {
                frameworks += DependencyInjectionFramework.HILT
                roles += ROLE_MODULE
                return ComponentAnnotationVisitor(delegate, components)
            }
            HILT_ENTRY_POINT,
            HILT_EARLY_ENTRY_POINT -> {
                frameworks += DependencyInjectionFramework.HILT
                roles += ROLE_ENTRY_POINT
                hiltEntryPoint = true
            }
            HILT_ANDROID_ENTRY_POINT,
            HILT_ANDROID_APP,
            HILT_VIEW_MODEL -> {
                frameworks += DependencyInjectionFramework.HILT
                roles += ROLE_CONSUMER
            }
            HILT_DEFINE_COMPONENT -> {
                frameworks += DependencyInjectionFramework.HILT
                roles += ROLE_COMPONENT
            }
            KOIN_MODULE -> {
                frameworks += DependencyInjectionFramework.KOIN
                roles += ROLE_MODULE
            }
            KOIN_COMPONENT_SCAN -> {
                frameworks += DependencyInjectionFramework.KOIN
                roles += ROLE_MODULE
            }
            in KOIN_DEFINITION_ANNOTATIONS -> {
                frameworks += DependencyInjectionFramework.KOIN
                roles += ROLE_BINDING
                roles += ROLE_CONSUMER
                koinClassDefinition = true
            }
        }
        if (annotation.isKnownScopeAnnotation()) scopes += annotation
        return delegate
    }

    override fun visitField(
        access: Int,
        name: String,
        descriptor: String,
        signature: String?,
        value: Any?,
    ): FieldVisitor? {
        val delegate = super.visitField(access, name, descriptor, signature, value)
        return object : FieldVisitor(Opcodes.ASM9, delegate) {
            private val annotations = linkedSetOf<String>()

            override fun visitAnnotation(annotationDescriptor: String, visible: Boolean): AnnotationVisitor? {
                annotations += annotationDescriptor.annotationClassName()
                return super.visitAnnotation(annotationDescriptor, visible)
            }

            override fun visitEnd() {
                if (annotations.containsInjectAnnotation()) {
                    roles += ROLE_CONSUMER
                    frameworks += DependencyInjectionFramework.DAGGER2
                    Type.getType(descriptor).dependencyClassName()?.let { dependency ->
                        addEdge(
                            consumer = dottedClassName,
                            dependency = dependency,
                            injectionKind = "field",
                            site = "$dottedClassName#$name:$descriptor",
                            qualifiers = annotations.qualifiers(),
                            resolution = RESOLUTION_DECLARED,
                        )
                    }
                }
                super.visitEnd()
            }
        }
    }

    override fun visitMethod(
        access: Int,
        name: String,
        descriptor: String,
        signature: String?,
        exceptions: Array<out String>?,
    ): MethodVisitor {
        val delegate = super.visitMethod(access, name, descriptor, signature, exceptions)
        return object : MethodVisitor(Opcodes.ASM9, delegate) {
            private val annotations = linkedSetOf<String>()
            private val parameterAnnotations = linkedMapOf<Int, MutableSet<String>>()

            override fun visitAnnotation(annotationDescriptor: String, visible: Boolean): AnnotationVisitor? {
                annotations += annotationDescriptor.annotationClassName()
                return super.visitAnnotation(annotationDescriptor, visible)
            }

            override fun visitParameterAnnotation(
                parameter: Int,
                annotationDescriptor: String,
                visible: Boolean,
            ): AnnotationVisitor? {
                parameterAnnotations.getOrPut(parameter, ::linkedSetOf) +=
                    annotationDescriptor.annotationClassName()
                return super.visitParameterAnnotation(parameter, annotationDescriptor, visible)
            }

            override fun visitEnd() {
                recordMethodDependencies(name, descriptor, annotations, parameterAnnotations)
                super.visitEnd()
            }
        }
    }

    override fun visitEnd() {
        val framework = primaryFramework()
        val classRecord = if (framework != null && (roles.isNotEmpty() || edges.isNotEmpty() || generated)) {
            DependencyInjectionClassRecord(
                name = dottedClassName,
                framework = framework,
                roles = roles.ifEmpty { setOf(ROLE_GENERATED_BINDING) },
                generated = generated,
                scopes = scopes,
                components = components,
            )
        } else {
            null
        }
        DependencyInjectionCatalogWriter.write(catalogDirectory, className, classRecord, edges)
        super.visitEnd()
    }

    private fun recordMethodDependencies(
        methodName: String,
        descriptor: String,
        annotations: Set<String>,
        parameterAnnotations: Map<Int, Set<String>>,
    ) {
        val argumentTypes = Type.getArgumentTypes(descriptor)
        val returnType = Type.getReturnType(descriptor)
        val site = "$dottedClassName#$methodName$descriptor"
        when {
            annotations.containsInjectAnnotation() -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_CONSUMER
                addArgumentEdges(
                    consumer = dottedClassName,
                    argumentTypes = argumentTypes,
                    injectionKind = if (methodName == "<init>") "constructor" else "method",
                    site = site,
                    methodAnnotations = annotations,
                    parameterAnnotations = parameterAnnotations,
                    resolution = RESOLUTION_DECLARED,
                )
            }
            annotations.contains(DAGGER_PROVIDES) -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_MODULE
                returnType.dependencyClassName()?.let { provided ->
                    addArgumentEdges(
                        consumer = provided,
                        argumentTypes = argumentTypes,
                        injectionKind = "provider",
                        site = site,
                        methodAnnotations = annotations,
                        parameterAnnotations = parameterAnnotations,
                        resolution = RESOLUTION_DECLARED,
                    )
                }
            }
            annotations.contains(DAGGER_BINDS) -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_MODULE
                returnType.dependencyClassName()?.let { binding ->
                    addArgumentEdges(
                        consumer = binding,
                        argumentTypes = argumentTypes.take(1).toTypedArray(),
                        injectionKind = "binding",
                        site = site,
                        methodAnnotations = annotations,
                        parameterAnnotations = parameterAnnotations,
                        resolution = RESOLUTION_DECLARED,
                    )
                }
            }
            annotations.any(KOIN_DEFINITION_ANNOTATIONS::contains) -> {
                frameworks += DependencyInjectionFramework.KOIN
                roles += ROLE_BINDING
                returnType.dependencyClassName()?.let { definition ->
                    addArgumentEdges(
                        consumer = definition,
                        argumentTypes = argumentTypes,
                        injectionKind = "koin_definition",
                        site = site,
                        methodAnnotations = annotations,
                        parameterAnnotations = parameterAnnotations,
                        resolution = RESOLUTION_DECLARED,
                        framework = DependencyInjectionFramework.KOIN,
                    )
                }
            }
            koinClassDefinition && methodName == "<init>" -> {
                addArgumentEdges(
                    consumer = dottedClassName,
                    argumentTypes = argumentTypes,
                    injectionKind = "koin_definition",
                    site = site,
                    methodAnnotations = annotations,
                    parameterAnnotations = parameterAnnotations,
                    resolution = RESOLUTION_DECLARED,
                    framework = DependencyInjectionFramework.KOIN,
                )
            }
            generated && methodName == "newInstance" -> {
                returnType.dependencyClassName()?.let { product ->
                    roles += ROLE_FACTORY
                    addArgumentEdges(
                        consumer = product,
                        argumentTypes = argumentTypes,
                        injectionKind = "generated_factory",
                        site = site,
                        methodAnnotations = annotations,
                        parameterAnnotations = parameterAnnotations,
                        resolution = RESOLUTION_GENERATED,
                    )
                }
            }
            generated && dottedClassName.endsWith("_MembersInjector") && methodName.startsWith("inject") -> {
                val target = argumentTypes.firstOrNull()?.dependencyClassName()
                if (target != null) {
                    roles += ROLE_MEMBERS_INJECTOR
                    addArgumentEdges(
                        consumer = target,
                        argumentTypes = argumentTypes.drop(1).toTypedArray(),
                        injectionKind = "members_injector",
                        site = site,
                        methodAnnotations = annotations,
                        parameterAnnotations = parameterAnnotations.mapKeys { (index, _) -> index - 1 },
                        resolution = RESOLUTION_GENERATED,
                    )
                }
            }
            hiltEntryPoint && methodName != "<init>" && argumentTypes.isEmpty() -> {
                returnType.dependencyClassName()?.let { dependency ->
                    addEdge(
                        consumer = dottedClassName,
                        dependency = dependency,
                        injectionKind = "entry_point",
                        site = site,
                        qualifiers = annotations.qualifiers(),
                        resolution = RESOLUTION_DECLARED,
                        framework = DependencyInjectionFramework.HILT,
                    )
                }
            }
        }
    }

    private fun addArgumentEdges(
        consumer: String,
        argumentTypes: Array<Type>,
        injectionKind: String,
        site: String,
        methodAnnotations: Set<String>,
        parameterAnnotations: Map<Int, Set<String>>,
        resolution: String,
        framework: DependencyInjectionFramework = primaryFramework() ?: DependencyInjectionFramework.DAGGER2,
    ) {
        argumentTypes.forEachIndexed { index, type ->
            type.dependencyClassName()?.let { dependency ->
                addEdge(
                    consumer = consumer,
                    dependency = dependency,
                    injectionKind = injectionKind,
                    site = site,
                    qualifiers = methodAnnotations.qualifiers() + parameterAnnotations[index].orEmpty().qualifiers(),
                    resolution = resolution,
                    framework = framework,
                )
            }
        }
    }

    private fun addEdge(
        consumer: String,
        dependency: String,
        injectionKind: String,
        site: String,
        qualifiers: Set<String>,
        resolution: String,
        framework: DependencyInjectionFramework = primaryFramework() ?: DependencyInjectionFramework.DAGGER2,
    ) {
        if (consumer == dependency) return
        edges += DependencyInjectionEdgeRecord(
            consumer = consumer,
            dependency = dependency,
            framework = framework,
            injectionKind = injectionKind,
            site = site,
            qualifiers = qualifiers,
            resolution = resolution,
        )
    }

    private fun primaryFramework(): DependencyInjectionFramework? {
        return when {
            DependencyInjectionFramework.HILT in frameworks -> DependencyInjectionFramework.HILT
            DependencyInjectionFramework.KOIN in frameworks -> DependencyInjectionFramework.KOIN
            DependencyInjectionFramework.DAGGER2 in frameworks -> DependencyInjectionFramework.DAGGER2
            else -> null
        }
    }

    private fun classifyGeneratedRole() {
        val simpleName = dottedClassName.substringAfterLast('.')
        when {
            simpleName.endsWith("_MembersInjector") -> roles += ROLE_MEMBERS_INJECTOR
            simpleName.endsWith("_Factory") -> roles += ROLE_FACTORY
            simpleName.startsWith("Dagger") -> roles += ROLE_COMPONENT
            simpleName.startsWith("Hilt_") || simpleName.contains("_HiltComponents") -> roles += ROLE_COMPONENT
            simpleName.endsWith("_HiltModules") -> roles += ROLE_GENERATED_MODULE
            simpleName.endsWith("ModuleGen") || dottedClassName.startsWith("org.koin.ksp.generated.") -> {
                roles += ROLE_GENERATED_MODULE
            }
            else -> roles += ROLE_GENERATED_BINDING
        }
    }

    private fun classifyGeneratedHierarchy(superName: String?, interfaces: Array<out String>) {
        val hierarchy = buildList {
            superName?.let(::add)
            addAll(interfaces)
        }.map { it.toDottedClassName() }
        when {
            hierarchy.contains("dagger.internal.Factory") -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_FACTORY
            }
            hierarchy.contains("dagger.MembersInjector") -> {
                frameworks += DependencyInjectionFramework.DAGGER2
                roles += ROLE_MEMBERS_INJECTOR
            }
        }
    }

    private class ComponentAnnotationVisitor(
        delegate: AnnotationVisitor?,
        private val components: MutableSet<String>,
    ) : AnnotationVisitor(Opcodes.ASM9, delegate) {
        override fun visit(name: String?, value: Any?) {
            if (value is Type) components += value.className
            super.visit(name, value)
        }

        override fun visitArray(name: String?): AnnotationVisitor {
            return ComponentAnnotationVisitor(super.visitArray(name), components)
        }
    }

    private companion object {
        private const val DAGGER_MODULE = "dagger.Module"
        private const val DAGGER_COMPONENT = "dagger.Component"
        private const val DAGGER_SUBCOMPONENT = "dagger.Subcomponent"
        private const val DAGGER_PROVIDES = "dagger.Provides"
        private const val DAGGER_BINDS = "dagger.Binds"
        private const val JAVAX_INJECT = "javax.inject.Inject"
        private const val JAKARTA_INJECT = "jakarta.inject.Inject"
        private const val JAVAX_SCOPE = "javax.inject.Scope"
        private const val JAKARTA_SCOPE = "jakarta.inject.Scope"
        private const val JAVAX_QUALIFIER = "javax.inject.Qualifier"
        private const val JAKARTA_QUALIFIER = "jakarta.inject.Qualifier"
        private const val HILT_INSTALL_IN = "dagger.hilt.InstallIn"
        private const val HILT_ENTRY_POINT = "dagger.hilt.EntryPoint"
        private const val HILT_EARLY_ENTRY_POINT = "dagger.hilt.android.EarlyEntryPoint"
        private const val HILT_ANDROID_ENTRY_POINT = "dagger.hilt.android.AndroidEntryPoint"
        private const val HILT_ANDROID_APP = "dagger.hilt.android.HiltAndroidApp"
        private const val HILT_VIEW_MODEL = "dagger.hilt.android.lifecycle.HiltViewModel"
        private const val HILT_DEFINE_COMPONENT = "dagger.hilt.DefineComponent"
        private const val KOIN_MODULE = "org.koin.core.annotation.Module"
        private const val KOIN_COMPONENT_SCAN = "org.koin.core.annotation.ComponentScan"

        private const val ROLE_CONSUMER = "consumer"
        private const val ROLE_MODULE = "module"
        private const val ROLE_COMPONENT = "component"
        private const val ROLE_SUBCOMPONENT = "subcomponent"
        private const val ROLE_ENTRY_POINT = "entry_point"
        private const val ROLE_FACTORY = "factory"
        private const val ROLE_MEMBERS_INJECTOR = "members_injector"
        private const val ROLE_BINDING = "binding"
        private const val ROLE_SCOPE = "scope"
        private const val ROLE_QUALIFIER = "qualifier"
        private const val ROLE_GENERATED_MODULE = "generated_module"
        private const val ROLE_GENERATED_BINDING = "generated_binding"

        private const val RESOLUTION_DECLARED = "declared"
        private const val RESOLUTION_GENERATED = "generated_confirmed"

        private val KOIN_DEFINITION_ANNOTATIONS = setOf(
            "org.koin.core.annotation.Single",
            "org.koin.core.annotation.Factory",
            "org.koin.core.annotation.Scoped",
            "org.koin.core.annotation.KoinViewModel",
            "org.koin.android.annotation.KoinViewModel",
            "org.koin.core.annotation.KoinWorker",
            "org.koin.android.annotation.KoinWorker",
        )

        private val KNOWN_SCOPE_ANNOTATIONS = setOf(
            "javax.inject.Singleton",
            "jakarta.inject.Singleton",
            "dagger.Reusable",
            "dagger.hilt.android.scopes.ActivityRetainedScoped",
            "dagger.hilt.android.scopes.ActivityScoped",
            "dagger.hilt.android.scopes.FragmentScoped",
            "dagger.hilt.android.scopes.ServiceScoped",
            "dagger.hilt.android.scopes.ViewScoped",
            "dagger.hilt.android.scopes.ViewWithFragmentScoped",
            "dagger.hilt.android.scopes.ViewModelScoped",
        )

        private val KNOWN_QUALIFIER_ANNOTATIONS = setOf(
            "javax.inject.Named",
            "jakarta.inject.Named",
            "org.koin.core.annotation.Named",
        )

        private val WRAPPER_TYPES = setOf(
            "javax.inject.Provider",
            "jakarta.inject.Provider",
            "dagger.Lazy",
            "kotlin.Lazy",
            "java.util.Optional",
        )

        private fun Set<String>.containsInjectAnnotation(): Boolean {
            return JAVAX_INJECT in this || JAKARTA_INJECT in this
        }

        private fun Set<String>.qualifiers(): Set<String> {
            return filterTo(linkedSetOf()) { it in KNOWN_QUALIFIER_ANNOTATIONS }
        }

        private fun String.isKnownScopeAnnotation(): Boolean = this in KNOWN_SCOPE_ANNOTATIONS

        private fun String.annotationClassName(): String = toDottedClassName().removePrefix("L").removeSuffix(";")

        private fun String.toDottedClassName(): String = replace('/', '.').trim()

        private fun Type.dependencyClassName(): String? {
            val name = when (sort) {
                Type.OBJECT -> className
                Type.ARRAY -> return elementType.dependencyClassName()
                else -> return null
            }
            return name.takeUnless(WRAPPER_TYPES::contains)
        }
    }
}
