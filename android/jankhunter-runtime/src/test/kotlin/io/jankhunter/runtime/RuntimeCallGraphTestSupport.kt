package io.jankhunter.runtime

internal fun RuntimeCallGraph.recordEdge(parentId: Long, childId: Long) {
    val parent = enter(parentId, enabled = true)
    val child = enter(childId, enabled = true)
    exit(child, childId)
    exit(parent, parentId)
}
