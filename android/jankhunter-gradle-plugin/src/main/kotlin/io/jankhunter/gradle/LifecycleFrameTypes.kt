package io.jankhunter.gradle

/** JVM verification types resolved from inputs; never use Gradle's class loader for Android classes. */
internal class LifecycleFrameTypes(private val index: LifecycleClassIndex) {
    fun common(first: String, second: String): String {
        if (assignable(first, second)) return first
        if (assignable(second, first)) return second
        if (first.startsWith('[') && second.startsWith('[')) {
            val a = referenceComponent(first)
            val b = referenceComponent(second)
            if (a != null && b != null) {
                val component = common(a, b)
                return "[" + if (component.startsWith('[')) component else "L$component;"
            }
            return OBJECT
        }
        if (first.startsWith('[') || second.startsWith('[')) return OBJECT
        for (parent in index.superclasses(first).drop(1)) {
            if (assignable(parent, second)) return parent
        }
        return OBJECT
    }

    private fun assignable(target: String, source: String): Boolean {
        if (target == source || target == OBJECT) return true
        if (source.startsWith('[')) {
            if (target == "java/lang/Cloneable" || target == "java/io/Serializable") return true
            if (!target.startsWith('[')) return false
            val a = referenceComponent(target) ?: return false
            val b = referenceComponent(source) ?: return false
            return assignable(a, b)
        }
        if (target.startsWith('[')) return false
        val pending = ArrayDeque<String>()
        val visited = HashSet<String>()
        pending.add(source)
        while (pending.isNotEmpty()) {
            val name = pending.removeLast()
            if (name == target) return true
            if (!visited.add(name)) continue
            if (name == OBJECT) continue
            val type = header(name)
            type.superName?.let(pending::add)
            type.interfaces.forEach(pending::add)
        }
        return false
    }

    private fun header(name: String): LifecycleClassHeader = checkNotNull(index.header(name)) {
        "Missing class metadata while computing lifecycle frames: $name"
    }

    private fun referenceComponent(array: String): String? = when (array.getOrNull(1)) {
        '[' -> array.substring(1)
        'L' -> array.substring(2, array.length - 1)
        else -> null
    }

    companion object {
        private const val OBJECT = "java/lang/Object"
    }
}
