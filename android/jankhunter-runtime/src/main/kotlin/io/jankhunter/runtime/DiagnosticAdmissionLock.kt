package io.jankhunter.runtime

import java.util.concurrent.locks.ReentrantLock

/** Diagnostic state may be absent under contention; application work never waits for this lock. */
internal inline fun <T> tryDiagnosticLock(lock: ReentrantLock, fallback: T, block: () -> T): T {
    if (!lock.tryLock()) {
        RuntimeHookFailureTracker.record(RuntimeHookFailureReason.REGISTRY_CONTENTION)
        return fallback
    }
    try { return block() } finally { lock.unlock() }
}
