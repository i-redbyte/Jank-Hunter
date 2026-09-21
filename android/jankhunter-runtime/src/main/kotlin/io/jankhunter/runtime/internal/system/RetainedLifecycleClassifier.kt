package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Dialog
import android.app.Service
import android.view.View
import io.jankhunter.runtime.BoundedWeakIdentityCache
import io.jankhunter.runtime.JankHunterBindingAccessor
import io.jankhunter.runtime.JankHunterLifecycleAccessorV1
import io.jankhunter.runtime.JankHunterLifecycleTargetSinkV1
import io.jankhunter.runtime.RuntimeHookGuard
import java.lang.reflect.Field
import java.lang.reflect.Method
import java.lang.reflect.Modifier

internal object RetainedLifecycleClassifier {
    private val classMetadata = BoundedWeakIdentityCache<Class<*>, LifecycleClassMetadata>(CLASS_CACHE_CAPACITY)

    fun visitTargets(instance: Any?, lifecycleEvent: String?, ownerHint: String?, sink: JankHunterLifecycleTargetSinkV1) {
        if (instance == null) return
        if (instance is JankHunterLifecycleAccessorV1) {
            visitTypedTargets(instance, instance.jankHunterLifecycleKindV1(), lifecycleEvent, ownerHint, sink)
            return
        }
        val event = normalizeEvent(lifecycleEvent)
        val metadata = metadata(instance.javaClass)
        val owner = ownerHint?.takeIf { it.isNotBlank() }
        when {
            event == "onDestroyView" && metadata.fragmentLike -> fragmentViewTargets(instance, metadata, owner, sink)
            event == "onDestroy" && instance is Activity && !isChangingConfigurations(instance) ->
                activityDestroyTargets(instance, owner, sink)
            event == "onDestroy" && metadata.fragmentLike -> emit(instance, owner, event, sink = sink)
            event == "onCleared" && metadata.viewModelLike -> emit(instance, owner, event, sink = sink)
            event == "onDestroy" && instance is Service -> emit(instance, owner, event, sink = sink)
            event == "onDestroy" && instance is Dialog -> {
                emit(instance, owner, event, sink = sink)
                decorView(instance)?.let { emit(it, owner, event, "${metadata.className}.decorView", sink) }
            }
        }
    }

    fun visitTypedTargets(instance: Any?, targetKind: Int, lifecycleEvent: String?, ownerHint: String?, sink: JankHunterLifecycleTargetSinkV1) {
        if (instance == null) return
        val event = normalizeEvent(lifecycleEvent)
        val owner = ownerHint?.takeIf { it.isNotBlank() }
        val kind = (instance as? JankHunterLifecycleAccessorV1)?.jankHunterLifecycleKindV1() ?: targetKind
        when {
            kind == 1 && event == "onDestroy" && instance is Activity && !isChangingConfigurations(instance) ->
                activityDestroyTargets(instance, owner, sink)
            kind == 2 && event == "onDestroy" -> emit(instance, owner, event, sink = sink)
            kind == 2 && event == "onDestroyView" && instance is JankHunterLifecycleAccessorV1 -> accessorTargets(instance, owner, sink)
            kind == 2 && event == "onDestroyView" -> fragmentViewTargets(instance, metadata(instance.javaClass), owner, sink)
            kind == 3 && event == "onCleared" -> emit(instance, owner, event, sink = sink)
            kind == 4 && event == "onDestroy" && instance is Service -> emit(instance, owner, event, sink = sink)
        }
    }

    private fun accessorTargets(instance: JankHunterLifecycleAccessorV1, owner: String?, destination: JankHunterLifecycleTargetSinkV1) {
        // Deduplication and admission belong to the bounded weak watcher, not a strong temporary set.
        val sink = JankHunterLifecycleTargetSinkV1 { value, hint ->
            if (value != null && value !== instance) emit(value, hint, "onDestroyView", sink = destination)
        }
        instance.jankHunterVisitLifecycleTargetsV1(sink, owner)
        RuntimeHookGuard.run { (instance as? JankHunterBindingAccessor)?.visitJankHunterBindings(sink, owner) }
    }

    private fun isChangingConfigurations(activity: Activity): Boolean = runCatching { activity.isChangingConfigurations }.getOrDefault(false)

    private fun activityDestroyTargets(activity: Activity, owner: String?, sink: JankHunterLifecycleTargetSinkV1) {
        emit(activity, owner, "onDestroy", sink = sink)
        decorView(activity)?.let { emit(it, owner, "onDestroy", "${activity.javaClass.name}.decorView", sink) }
    }

    private fun fragmentViewTargets(fragment: Any, metadata: LifecycleClassMetadata, owner: String?, sink: JankHunterLifecycleTargetSinkV1) {
        val name = metadata.className
        currentFragmentView(fragment, metadata.viewGetter)?.let { emit(it, owner, "onDestroyView", "$name.view", sink) }
        for (field in metadata.candidateFields) {
            var value = runCatching { field.isAccessible = true; field.get(fragment) }.getOrNull() ?: continue
            if (value is Lazy<*>) {
                if (!value.isInitialized()) continue
                value = value.value ?: continue
            }
            if (value === fragment) continue
            val binding = metadata.bindingRootGetter?.declaringClass?.isInstance(value) == true
            if (value !is View && !binding) continue
            emit(value, owner, "onDestroyView", "$name.${field.name}", sink)
            if (binding) {
                val root = runCatching { metadata.bindingRootGetter.invoke(value) as? View }.getOrNull()
                if (root != null) emit(root, owner, "onDestroyView", "$name.${field.name}.root", sink)
            }
        }
    }

    private fun emit(instance: Any, owner: String?, event: String, source: String = instance.javaClass.name, sink: JankHunterLifecycleTargetSinkV1) {
        sink.accept(instance, owner ?: "lifecycle.$event.${source.takeIf { it.isNotBlank() } ?: instance.javaClass.name}")
    }

    private fun currentFragmentView(fragment: Any, getter: Method?): View? {
        return runCatching {
            getter?.invoke(fragment) as? View
        }.getOrNull()
    }

    private fun lifecycleCandidateFields(type: Class<*>, frameworkRoot: Class<*>): Array<Field> {
        val out = ArrayList<Field>()
        var current: Class<*>? = type
        while (current != null && current !== frameworkRoot && current !== Any::class.java) {
            for (field in current.declaredFields) {
                if (Modifier.isStatic(field.modifiers)) continue
                if (!field.type.isPrimitive && !field.type.isArray) {
                    out += field
                }
            }
            current = current.superclass
        }
        return out.toTypedArray()
    }

    private fun decorView(activity: Activity): View? {
        return runCatching { activity.window?.decorView }.getOrNull()
    }

    private fun decorView(dialog: Dialog): View? {
        return runCatching { dialog.window?.decorView }.getOrNull()
    }

    private fun metadata(type: Class<*>): LifecycleClassMetadata {
        return classMetadata.getOrPut(type) {
            // Consumer rules preserve these optional framework types and reflected methods.
            // Application fields remain best-effort on this partial legacy path.
            val fragmentType = listOfNotNull(
                optionalClass { Class.forName("android.app.Fragment", false, type.classLoader) },
                optionalClass { Class.forName("androidx.fragment.app.Fragment", false, type.classLoader) },
                optionalClass { Class.forName("android.support.v4.app.Fragment", false, type.classLoader) },
            ).firstOrNull { it.isAssignableFrom(type) }
            val viewModelLike = optionalClass {
                Class.forName("androidx.lifecycle.ViewModel", false, type.classLoader)
            }?.isAssignableFrom(type) == true || optionalClass {
                Class.forName("android.arch.lifecycle.ViewModel", false, type.classLoader)
            }?.isAssignableFrom(type) == true
            val bindingType = optionalClass { Class.forName("androidx.viewbinding.ViewBinding", false, type.classLoader) }
            LifecycleClassMetadata(
                className = type.name,
                fragmentLike = fragmentType != null,
                viewModelLike = viewModelLike,
                viewGetter = runCatching { fragmentType?.getMethod("getView") }.getOrNull(),
                bindingRootGetter = runCatching { bindingType?.getMethod("getRoot") }.getOrNull(),
                candidateFields = if (fragmentType != null) lifecycleCandidateFields(type, fragmentType) else emptyArray(),
            )
        }
    }

    private inline fun optionalClass(resolve: () -> Class<*>): Class<*>? = try {
        resolve()
    } catch (_: ClassNotFoundException) {
        null
    } catch (_: LinkageError) {
        null
    }

    private fun normalizeEvent(value: String?): String {
        return value?.trim()?.takeIf { it.isNotEmpty() } ?: "lifecycle"
    }

    private class LifecycleClassMetadata(
        val className: String,
        val fragmentLike: Boolean,
        val viewModelLike: Boolean,
        val viewGetter: Method?,
        val bindingRootGetter: Method?,
        val candidateFields: Array<Field>,
    )

    private const val CLASS_CACHE_CAPACITY = 32
}
