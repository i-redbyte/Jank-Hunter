package io.jankhunter.sample.benchmarkcontrol

import java.util.concurrent.Callable
import java.util.concurrent.ExecutorService
import java.util.concurrent.Future

/** Benchmark control: identical platform submission without an injected submission hook. */
internal fun <T> submitUnobserved(executor: ExecutorService, task: Callable<T>): Future<T> = executor.submit(task)
