package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Dialog
import android.app.Service
import android.view.View
import io.jankhunter.runtime.BoundedWeakIdentityCache
import java.lang.reflect.Field
import java.lang.reflect.Method
import java.lang.reflect.Modifier
import java.util.Collections

internal data class RetainedLifecycleTarget(
    val instance: Any,
    val description: String,
    val ownerHint: String,
)

internal object RetainedLifecycleClassifier {
    private val classMetadata = BoundedWeakIdentityCache<Class<*>, LifecycleClassMetadata>(CLASS_CACHE_CAPACITY)

    fun targets(instance: Any?, lifecycleEvent: String?, ownerHint: String?): List<RetainedLifecycleTarget> {
        if (instance == null) return emptyList()
        val event = normalizeEvent(lifecycleEvent)
        val metadata = metadata(instance.javaClass)
        val className = metadata.className
        val explicitOwner = ownerHint?.takeIf { it.isNotBlank() }
        return when {
            event == "onDestroyView" && metadata.fragmentLike -> {
                fragmentViewTargets(instance, metadata, explicitOwner)
            }
            event == "onDestroy" && instance is Activity && !isChangingConfigurations(instance) -> {
                activityDestroyTargets(instance, className, explicitOwner)
            }
            event == "onDestroy" && metadata.fragmentLike -> single(instance, className, explicitOwner, "fragment", event)
            event == "onCleared" && metadata.viewModelLike -> single(instance, className, explicitOwner, "viewmodel", event)
            event == "onDestroy" && instance is Service -> single(instance, className, explicitOwner, "service", event)
            event == "onDestroy" && instance is Dialog -> dialogTargets(instance, className, explicitOwner, event)
            else -> emptyList()
        }
    }

    private fun isChangingConfigurations(activity: Activity): Boolean {
        return runCatching { activity.isChangingConfigurations }.getOrDefault(false)
    }

    private fun single(
        instance: Any,
        className: String,
        ownerHint: String?,
        kind: String,
        event: String,
    ): List<RetainedLifecycleTarget> {
        return listOf(target(instance, className, ownerHint, kind, event, className))
    }

    private fun activityDestroyTargets(activity: Activity, className: String, ownerHint: String?): List<RetainedLifecycleTarget> {
        val out = mutableListOf(target(activity, className, ownerHint, "activity", "onDestroy", className))
        decorView(activity)?.let { view ->
            out += target(view, view.javaClass.name, ownerHint, "activity_decor_view", "onDestroy", "$className.decorView")
        }
        return out
    }

    private fun dialogTargets(dialog: Dialog, className: String, ownerHint: String?, event: String): List<RetainedLifecycleTarget> {
        val out = mutableListOf(target(dialog, className, ownerHint, "dialog", event, className))
        decorView(dialog)?.let { view ->
            out += target(view, view.javaClass.name, ownerHint, "dialog_decor_view", event, "$className.decorView")
        }
        return out
    }

    private fun fragmentViewTargets(
        fragment: Any,
        metadata: LifecycleClassMetadata,
        ownerHint: String?,
    ): List<RetainedLifecycleTarget> {
        val fragmentClassName = metadata.className
        val out = ArrayList<RetainedLifecycleTarget>()
        val seen = Collections.newSetFromMap(java.util.IdentityHashMap<Any, Boolean>())
        currentFragmentView(fragment, metadata.viewGetter)?.let { view ->
            if (seen.add(view)) {
                out += target(
                    view,
                    view.javaClass.name,
                    ownerHint,
                    "fragment_view",
                    "onDestroyView",
                    "$fragmentClassName.view",
                )
            }
        }
        for (field in metadata.candidateFields) {
            val value = runCatching {
                field.isAccessible = true
                field.get(fragment)
            }.getOrNull() ?: continue
            if (value === fragment || !seen.add(value)) continue
            val kind = when {
                value is View -> "fragment_view"
                isBindingLike(value) -> "fragment_binding"
                else -> continue
            }
            out += target(
                value,
                value.javaClass.name,
                ownerHint,
                kind,
                "onDestroyView",
                "$fragmentClassName.${field.name}",
            )
        }
        return out
    }

    private fun target(
        instance: Any,
        description: String,
        ownerHint: String?,
        kind: String,
        event: String,
        source: String,
    ): RetainedLifecycleTarget {
        val cleanSource = source.takeIf { it.isNotBlank() } ?: description
        return RetainedLifecycleTarget(
            instance = instance,
            description = description,
            ownerHint = ownerHint ?: "lifecycle.$event.$cleanSource",
        )
    }

    private fun currentFragmentView(fragment: Any, getter: Method?): View? {
        return runCatching {
            getter?.invoke(fragment) as? View
        }.getOrNull()
    }

    private fun lifecycleCandidateFields(type: Class<*>): Array<Field> {
        val out = ArrayList<Field>()
        var current: Class<*>? = type
        while (current != null && !current.name.startsWith("android.") && current.name != "java.lang.Object") {
            for (field in current.declaredFields) {
                if (Modifier.isStatic(field.modifiers)) continue
                val name = field.name.lowercase()
                val fieldType = field.type.name.lowercase()
                if (
                    "binding" in name ||
                    name == "itemview" ||
                    name == "view" ||
                    name.endsWith("view") ||
                    "binding" in fieldType ||
                    field.type == View::class.java ||
                    View::class.java.isAssignableFrom(field.type)
                ) {
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

    private fun isBindingLike(instance: Any): Boolean {
        val name = instance.javaClass.name
        return name.endsWith("Binding") || name.contains(".databinding.") || name.contains("ViewBinding")
    }

    private fun metadata(type: Class<*>): LifecycleClassMetadata {
        return classMetadata.getOrPut(type) {
            var fragmentLike = type.name.contains("Fragment")
            var viewModelLike = type.name.contains("ViewModel")
            var current: Class<*>? = type
            while (current != null) {
                when (current.name) {
                    "android.app.Fragment",
                    "androidx.fragment.app.Fragment",
                    "android.support.v4.app.Fragment",
                    -> fragmentLike = true
                    "androidx.lifecycle.ViewModel",
                    "android.arch.lifecycle.ViewModel",
                    -> viewModelLike = true
                }
                current = current.superclass
            }
            LifecycleClassMetadata(
                className = type.name,
                fragmentLike = fragmentLike,
                viewModelLike = viewModelLike,
                viewGetter = if (fragmentLike) {
                    type.methods.firstOrNull { it.name == "getView" && it.parameterTypes.isEmpty() }
                } else {
                    null
                },
                candidateFields = if (fragmentLike) lifecycleCandidateFields(type) else emptyArray(),
            )
        }
    }

    private fun normalizeEvent(value: String?): String {
        return value?.trim()?.takeIf { it.isNotEmpty() } ?: "lifecycle"
    }

    private class LifecycleClassMetadata(
        val className: String,
        val fragmentLike: Boolean,
        val viewModelLike: Boolean,
        val viewGetter: Method?,
        val candidateFields: Array<Field>,
    )

    private const val CLASS_CACHE_CAPACITY = 32
}
