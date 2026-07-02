package io.jankhunter.runtime.internal.system

import android.app.Activity
import android.app.Dialog
import android.app.Service
import android.view.View
import java.lang.reflect.Field
import java.lang.reflect.Modifier
import java.util.Collections

internal data class RetainedLifecycleTarget(
    val instance: Any,
    val description: String,
    val ownerHint: String,
    val flow: String,
    val step: String,
)

internal object RetainedLifecycleClassifier {
    private const val MAX_ASSOCIATED_TARGETS = 8

    fun targets(instance: Any?, lifecycleEvent: String?, ownerHint: String?): List<RetainedLifecycleTarget> {
        if (instance == null) return emptyList()
        val event = normalizeEvent(lifecycleEvent)
        val className = instance.javaClass.name
        val explicitOwner = ownerHint?.takeIf { it.isNotBlank() }
        return when {
            event == "onDestroyView" && isFragmentLike(instance) -> fragmentViewTargets(instance, className, explicitOwner)
            event == "onDestroy" && instance is Activity -> activityDestroyTargets(instance, className, explicitOwner)
            event == "onDestroy" && isFragmentLike(instance) -> single(instance, className, explicitOwner, "fragment", event)
            event == "onCleared" && isViewModelLike(instance) -> single(instance, className, explicitOwner, "viewmodel", event)
            event == "onDetachedFromWindow" && instance is View && !isFrameworkLifecycleClass(className) -> {
                single(instance, className, explicitOwner, "view", event)
            }
            event == "onDestroy" && instance is Service -> single(instance, className, explicitOwner, "service", event)
            event in setOf("onStop", "onDestroy") && instance is Dialog -> dialogTargets(instance, className, explicitOwner, event)
            event in setOf("onViewRecycled", "onViewDetachedFromWindow") && isViewHolderLike(instance) -> {
                viewHolderTargets(instance, className, explicitOwner, event)
            }
            event == "onDetachedFromRecyclerView" && isRecyclerAdapterLike(instance) -> {
                single(instance, className, explicitOwner, "recycler_adapter", event)
            }
            else -> emptyList()
        }
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

    private fun fragmentViewTargets(fragment: Any, fragmentClassName: String, ownerHint: String?): List<RetainedLifecycleTarget> {
        val out = ArrayList<RetainedLifecycleTarget>()
        val seen = Collections.newSetFromMap(java.util.IdentityHashMap<Any, Boolean>())
        currentFragmentView(fragment)?.let { view ->
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
        for (field in fragmentCandidateFields(fragment.javaClass)) {
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
            if (out.size >= MAX_ASSOCIATED_TARGETS) break
        }
        return out
    }

    private fun viewHolderTargets(viewHolder: Any, className: String, ownerHint: String?, event: String): List<RetainedLifecycleTarget> {
        val out = ArrayList<RetainedLifecycleTarget>()
        val seen = Collections.newSetFromMap(java.util.IdentityHashMap<Any, Boolean>())
        for (field in lifecycleCandidateFields(viewHolder.javaClass)) {
            val value = runCatching {
                field.isAccessible = true
                field.get(viewHolder)
            }.getOrNull() ?: continue
            if (value === viewHolder || !seen.add(value)) continue
            val kind = when {
                value is View -> "viewholder_view"
                isBindingLike(value) -> "viewholder_binding"
                else -> continue
            }
            out += target(value, value.javaClass.name, ownerHint, kind, event, "$className.${field.name}")
            if (out.size >= MAX_ASSOCIATED_TARGETS) break
        }
        if (out.isEmpty()) {
            out += target(viewHolder, className, ownerHint, "viewholder", event, className)
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
            flow = lifecycleFlow(kind),
            step = event,
        )
    }

    private fun lifecycleFlow(kind: String): String = "lifecycle.autowatch.$kind"

    private fun currentFragmentView(fragment: Any): View? {
        return runCatching {
            val method = fragment.javaClass.methods.firstOrNull {
                it.name == "getView" && it.parameterTypes.isEmpty()
            } ?: return null
            method.invoke(fragment) as? View
        }.getOrNull()
    }

    private fun fragmentCandidateFields(type: Class<*>): List<Field> {
        return lifecycleCandidateFields(type)
    }

    private fun lifecycleCandidateFields(type: Class<*>): List<Field> {
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
        return out
    }

    private fun decorView(activity: Activity): View? {
        return runCatching { activity.window?.decorView }.getOrNull()
    }

    private fun decorView(dialog: Dialog): View? {
        return runCatching { dialog.window?.decorView }.getOrNull()
    }

    private fun isFragmentLike(instance: Any): Boolean {
        return ancestryNames(instance.javaClass).any {
            it == "android.app.Fragment" ||
                it == "androidx.fragment.app.Fragment" ||
                it == "android.support.v4.app.Fragment"
        } || instance.javaClass.name.contains("Fragment")
    }

    private fun isViewModelLike(instance: Any): Boolean {
        return ancestryNames(instance.javaClass).any {
            it == "androidx.lifecycle.ViewModel" ||
                it == "android.arch.lifecycle.ViewModel"
        } || instance.javaClass.name.contains("ViewModel")
    }

    private fun isViewHolderLike(instance: Any): Boolean {
        return ancestryNames(instance.javaClass).any {
            it == "androidx.recyclerview.widget.RecyclerView.ViewHolder" ||
                it == "android.support.v7.widget.RecyclerView.ViewHolder"
        } || instance.javaClass.name.contains("ViewHolder")
    }

    private fun isRecyclerAdapterLike(instance: Any): Boolean {
        return ancestryNames(instance.javaClass).any {
            it == "androidx.recyclerview.widget.RecyclerView.Adapter" ||
                it == "android.support.v7.widget.RecyclerView.Adapter"
        } || instance.javaClass.name.contains("Adapter")
    }

    private fun isBindingLike(instance: Any): Boolean {
        val name = instance.javaClass.name
        return name.endsWith("Binding") || name.contains(".databinding.") || name.contains("ViewBinding")
    }

    private fun isFrameworkLifecycleClass(className: String): Boolean {
        return className.startsWith("android.") ||
            className.startsWith("androidx.") ||
            className.startsWith("com.android.")
    }

    private fun ancestryNames(type: Class<*>): Sequence<String> {
        return generateSequence(type) { it.superclass }.map { it.name }
    }

    private fun normalizeEvent(value: String?): String {
        return value?.trim()?.takeIf { it.isNotEmpty() } ?: "lifecycle"
    }
}
