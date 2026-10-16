package io.jankhunter.runtime

import java.lang.ref.WeakReference

/** Bounded, epoch-owned evidence of publicly observed WorkContinuation construction. */
internal class RuntimeWorkChainTracker(
    private val capacity: Int = 64,
    private val maxTailIds: Int = 8,
) {
    data class Id(val most: Long, val least: Long)

    private class Node(continuation: Any) {
        val reference = WeakReference(continuation)
        val identity = System.identityHashCode(continuation)
        val tails = ArrayList<Id>()
    }

    private val nodes = ArrayList<Node>()

    var omittedIds: Long = 0
        private set

    init {
        require(capacity in 1..256 && maxTailIds in 1..128)
    }

    fun root(continuation: Any, most: Long, least: Long): Boolean = synchronized(this) {
        val node = node(continuation) ?: return@synchronized omitted()
        add(node, Id(most, least))
    }

    /** A returned continuation holds child tails. Missing parent evidence yields no invented edge. */
    fun then(
        parent: Any,
        continuation: Any,
        most: Long,
        least: Long,
        edge: (Id, Id) -> Unit,
    ): Boolean {
        val child = Id(most, least)
        val parentTails: List<Id>
        synchronized(this) {
            parentTails = find(parent)?.tails?.toList().orEmpty()
            val node = node(continuation) ?: return omitted()
            if (!add(node, child)) return false
        }
        parentTails.forEach { edge(it, child) }
        return true
    }

    /** A combine declaration merges only tails already observed in this collection epoch. */
    fun combine(parents: List<*>, continuation: Any): Boolean {
        if (!isMaterialized(parents)) return false
        // Read host-owned data before acquiring the evidence lock. Never call an opaque List.
        val snapshot = parents.take(MAX_PARENTS)
        return combineSnapshot(snapshot, continuation)
    }

    private fun combineSnapshot(parents: List<*>, continuation: Any): Boolean = synchronized(this) {
        val tails = ArrayList<Id>(maxTailIds)
        for (index in 0 until minOf(parents.size, MAX_PARENTS)) {
            val parent = parents[index] ?: continue
            for (id in find(parent)?.tails.orEmpty()) {
                if (id !in tails) {
                    if (tails.size >= maxTailIds) return@synchronized omitted()
                    tails.add(id)
                }
            }
        }
        val node = node(continuation) ?: return@synchronized omitted()
        if (node.tails.size + tails.count { it !in node.tails } > maxTailIds) return@synchronized omitted()
        tails.forEach { add(node, it) }
        true
    }

    private fun omitted(): Boolean {
        if (omittedIds < Long.MAX_VALUE) omittedIds++
        return false
    }

    private fun add(node: Node, id: Id): Boolean {
        if (node.tails.contains(id)) return true
        if (node.tails.size >= maxTailIds) return omitted()
        node.tails.add(id)
        return true
    }

    private fun node(continuation: Any): Node? {
        find(continuation)?.let { return it }
        if (nodes.size >= capacity) return null
        return Node(continuation).also(nodes::add)
    }

    private fun find(continuation: Any): Node? {
        var index = 0
        while (index < nodes.size) {
            val node = nodes[index]
            val current = node.reference.get()
            if (current == null) {
                nodes.removeAt(index)
            } else {
                if (node.identity == System.identityHashCode(continuation) && current === continuation) return node
                index++
            }
        }
        return null
    }

    private companion object { const val MAX_PARENTS = 8 }
}
