package io.jankhunter.runtime

import android.os.Build
import java.lang.reflect.InvocationTargetException
import java.lang.reflect.Method
import java.lang.reflect.Modifier
import java.time.Duration
import java.util.UUID
import java.util.concurrent.locks.ReentrantLock
import java.util.concurrent.TimeUnit

/** Optional WorkManager metadata boundary. It is loaded only after an observed WorkManager call. */
internal object WorkChainRequestCapture {
    private const val MAX_REQUESTS = 8
    private const val MAX_SUBMISSIONS = 128
    private const val MAX_BINDINGS = 8
    private const val MAX_HIERARCHY_DEPTH = 32
    private val bindingLock = ReentrantLock()
    private val bindings = HashMap<Class<*>, Method>(MAX_BINDINGS)

    internal fun requestId(request: Any): UUID {
        val base = requireNotNull(frameworkBase(request.javaClass)) { "unrecognized work request" }
        val concreteGetter = request.javaClass.getMethod("getId")
        require(concreteGetter.declaringClass == base && Modifier.isFinal(request.javaClass.modifiers)) {
            "overridable work identity getter"
        }
        if (!bindingLock.tryLock()) error("work identity binding contention")
        val cached = try {
            bindings[base] ?: run {
                if (bindings.size >= MAX_BINDINGS) error("work identity binding capacity")
                null
            }
        } finally { bindingLock.unlock() }
        val getter = cached ?: base.getMethod("getId").also { method ->
            require(method.returnType == UUID::class.java) {
                "unsupported work identity getter"
            }
            if (!bindingLock.tryLock()) error("work identity binding contention")
            try { if (bindings.size < MAX_BINDINGS) bindings[base] = method }
            finally { bindingLock.unlock() }
        }
        return try { getter.invoke(request) as UUID }
        catch (failure: InvocationTargetException) { throw failure.targetException }
    }

    private fun frameworkBase(type: Class<*>): Class<*>? {
        var current: Class<*>? = type
        repeat(MAX_HIERARCHY_DEPTH) {
            val value = current ?: return null
            if (value.name == "androidx.work.WorkRequest") return value
            current = value.superclass
        }
        return null
    }

    private fun periodic(request: Any): Boolean {
        var current: Class<*>? = request.javaClass
        repeat(MAX_HIERARCHY_DEPTH) {
            val value = current ?: return false
            if (value.name == "androidx.work.PeriodicWorkRequest") return true
            current = value.superclass
        }
        return false
    }

    fun backoff(builder: Any, policy: Any?, delay: Long, unit: Any?) {
        val code = when ((policy as? Enum<*>)?.name) { "LINEAR" -> 1; "EXPONENTIAL" -> 2; else -> 0 }
        val millis = (unit as? TimeUnit)?.toMillis(delay) ?: -1L
        JankHunterHooks.observeWorkBuilderBackoff(builder, code, millis)
    }

    fun backoffDuration(builder: Any, policy: Any?, duration: Any?) {
        val millis = if (Build.VERSION.SDK_INT >= 26) (duration as? Duration)?.toMillis() ?: -1L else -1L
        backoff(builder, policy, millis, TimeUnit.MILLISECONDS)
    }

    fun expedited(builder: Any, policy: Any?) {
        val code = when ((policy as? Enum<*>)?.name) {
            "RUN_AS_NON_EXPEDITED_WORK_REQUEST" -> 1
            "DROP_WORK_REQUEST" -> 2
            else -> 0
        }
        JankHunterHooks.observeWorkBuilderExpedited(builder, code)
    }

    fun built(builder: Any, request: Any?) {
        val id = request?.let(::requestId) ?: return
        JankHunterHooks.observeWorkBuilderBuilt(builder, id.mostSignificantBits, id.leastSignificantBits)
    }

    fun root(epoch: Long, continuation: Any, requestOrList: Any?, uniqueName: Any?, policy: Any?, source: String) {
        captureRequests(requestOrList, MAX_REQUESTS, { JankHunterHooks.workChainOmitted(epoch, it) },
            { chainFailure(epoch, it) }) { request, _, _ ->
            val id = requestId(request)
            JankHunterHooks.observeWorkChainRoot(epoch, continuation, id.mostSignificantBits, id.leastSignificantBits,
                uniqueName as? String, (policy as? Enum<*>)?.name, source)
        }
    }

    fun child(epoch: Long, parent: Any, continuation: Any, requestOrList: Any?, source: String) {
        captureRequests(requestOrList, MAX_REQUESTS, { JankHunterHooks.workChainOmitted(epoch, it) },
            { chainFailure(epoch, it) }) { request, _, _ ->
            val id = requestId(request)
            JankHunterHooks.observeWorkChainChild(epoch, parent, continuation,
                id.mostSignificantBits, id.leastSignificantBits, source)
        }
    }

    fun submission(epoch: Long, requestOrList: Any?, uniqueName: Any?, policy: Any?, source: String) {
        var omittedPending = 0
        captureRequests(requestOrList, MAX_SUBMISSIONS, { omittedPending = it },
            JankHunterHooks::workSubmissionCaptureFailed) { request, _, _ ->
            val id = requestId(request)
            JankHunterHooks.observeWorkSubmission(epoch, id.mostSignificantBits, id.leastSignificantBits,
                periodic(request), uniqueName as? String, (policy as? Enum<*>)?.name,
                omittedPending, source)
            omittedPending = 0
        }
    }

    private fun chainFailure(epoch: Long, failure: Throwable) {
        JankHunterHooks.workChainOmitted(epoch, 1)
        JankHunterHooks.workSubmissionCaptureFailed(failure)
    }

    internal fun captureRequests(
        requestOrList: Any?, limit: Int, overflow: (Int) -> Unit,
        failure: (Throwable) -> Unit, observed: (Any, Int, Int) -> Unit,
    ) {
        try {
            val requests = requestOrList as? List<*>
            if (requests != null && !isMaterialized(requests)) error("opaque work submission collection")
            val count = requests?.size ?: 1
            if (count > limit) overflow(count - limit)
            for (index in 0 until minOf(count, limit)) {
                try {
                    val request = if (requests == null) requestOrList else requests[index]
                    val known = requireNotNull(request)
                    require(frameworkBase(known.javaClass) != null) { "unrecognized work request" }
                    observed(known, index, count)
                } catch (error: Throwable) {
                    RuntimeHookGuard.rethrowFatal(error)
                    failure(error)
                }
            }
        } catch (error: Throwable) {
            RuntimeHookGuard.rethrowFatal(error)
            failure(error)
        }
    }
}
