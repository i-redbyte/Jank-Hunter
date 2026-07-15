package io.jankhunter.runtime.internal.io

/**
 * A bounded, structure-of-arrays hand-off from the runtime graph to the writer thread.
 *
 * The graph produces at most one instance per flush pass. Keeping the hot-path aggregation in
 * primitive arrays and batching the cold-path queue hand-off prevents a graph flush from filling
 * the writer queue with thousands of individual objects.
 */
internal class RuntimeCallBatch(capacity: Int) {
    private val screens = arrayOfNulls<String>(capacity)
    private val callers = LongArray(capacity)
    private val callerNames = arrayOfNulls<String>(capacity)
    private val flows = arrayOfNulls<String>(capacity)
    private val steps = arrayOfNulls<String>(capacity)
    private val callees = LongArray(capacity)
    private val calleeNames = arrayOfNulls<String>(capacity)
    private val counts = LongArray(capacity)
    private val totalsMs = LongArray(capacity)
    private val maximaMs = LongArray(capacity)

    var size: Int = 0
        private set

    fun add(
        screen: String?,
        callerId: Long,
        flow: String?,
        step: String?,
        calleeId: Long,
        count: Long,
        totalMs: Long,
        maxMs: Long,
    ) = add(screen, callerId, null, flow, step, calleeId, null, count, totalMs, maxMs)

    fun add(
        screen: String?,
        callerId: Long,
        callerName: String?,
        flow: String?,
        step: String?,
        calleeId: Long,
        calleeName: String?,
        count: Long,
        totalMs: Long,
        maxMs: Long,
    ) {
        check(size < callers.size) { "Runtime call batch capacity exceeded" }
        val index = size++
        screens[index] = screen
        callers[index] = callerId
        callerNames[index] = callerName
        flows[index] = flow
        steps[index] = step
        callees[index] = calleeId
        calleeNames[index] = calleeName
        counts[index] = count
        totalsMs[index] = totalMs
        maximaMs[index] = maxMs
    }

    fun screen(index: Int): String? = screens[index]

    fun callerId(index: Int): Long = callers[index]

    fun callerName(index: Int): String? = callerNames[index]

    fun flow(index: Int): String? = flows[index]

    fun step(index: Int): String? = steps[index]

    fun calleeId(index: Int): Long = callees[index]

    fun calleeName(index: Int): String? = calleeNames[index]

    fun count(index: Int): Long = counts[index]

    fun totalMs(index: Int): Long = totalsMs[index]

    fun maxMs(index: Int): Long = maximaMs[index]
}
