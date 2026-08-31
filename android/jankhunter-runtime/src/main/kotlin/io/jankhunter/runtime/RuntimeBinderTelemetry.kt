package io.jankhunter.runtime

import android.os.DeadObjectException
import android.os.Looper
import android.os.RemoteException
import io.jankhunter.runtime.internal.io.Jhlog
import java.util.concurrent.TimeoutException

/** Binder metadata only: this class never reads or mutates Parcel payloads. */
internal class RuntimeBinderTelemetry(
    private val access: RuntimeTelemetryAccess,
    nowUs: RuntimeLongSource,
) {
    private val tokens = RuntimeTraceTokenSource(nowUs)

    fun enter(): Long = if (access.isActive() && access.writer != null) tokens.start() else 0L

    fun exitClient(
        token: Long,
        descriptor: String?,
        method: String?,
        code: Int,
        flags: Int,
        handled: Boolean,
        throwable: Throwable?,
    ) {
        exit(token, descriptor, method, code, flags, handled, throwable, Jhlog.BINDER_DIRECTION_CLIENT)
    }

    fun exitServer(
        token: Long,
        descriptor: String?,
        method: String?,
        code: Int,
        flags: Int,
        handled: Boolean,
        throwable: Throwable?,
    ) {
        exit(token, descriptor, method, code, flags, handled, throwable, Jhlog.BINDER_DIRECTION_SERVER)
    }

    private fun exit(
        token: Long,
        descriptor: String?,
        method: String?,
        code: Int,
        flags: Int,
        handled: Boolean,
        throwable: Throwable?,
        direction: Long,
    ) {
        if (token == 0L) return
        val writer = access.writer ?: return
        val failureKind = binderFailureKind(throwable)
        val outcome = when {
            throwable != null -> Jhlog.BINDER_OUTCOME_FAILURE
            !handled -> Jhlog.BINDER_OUTCOME_UNHANDLED
            else -> Jhlog.BINDER_OUTCOME_SUCCESS
        }
        writer.binderTransaction(
            descriptor = descriptor,
            method = method,
            callId = token,
            direction = direction,
            transactionCode = code.toLong() and UINT_MASK,
            outcome = outcome,
            failureKind = failureKind,
            durationUs = RuntimeTraceTokenSource.durationUs(token, tokens.nowUs()),
            binderFlags = binderEventFlags(flags),
            mainThread = Looper.myLooper() === Looper.getMainLooper(),
        )
    }
}

internal fun binderEventFlags(flags: Int): Long {
    return if (flags and BINDER_FLAG_ONEWAY != 0) Jhlog.BINDER_FLAG_ONEWAY else 0L
}

internal fun binderFailureKind(throwable: Throwable?): Long {
    var current = throwable
    var depth = 0
    while (current != null && depth++ < MAX_CAUSE_DEPTH) {
        when (current) {
            is DeadObjectException -> return Jhlog.BINDER_FAILURE_DEAD_OBJECT
            is RemoteException -> return Jhlog.BINDER_FAILURE_REMOTE
            is SecurityException -> return Jhlog.BINDER_FAILURE_SECURITY
            is TimeoutException -> return Jhlog.BINDER_FAILURE_TIMEOUT
        }
        current = current.cause
    }
    return if (throwable == null) Jhlog.BINDER_FAILURE_NONE else Jhlog.BINDER_FAILURE_OTHER
}

private const val BINDER_FLAG_ONEWAY = 1
private const val MAX_CAUSE_DEPTH = 8
private const val UINT_MASK = 0xffff_ffffL
