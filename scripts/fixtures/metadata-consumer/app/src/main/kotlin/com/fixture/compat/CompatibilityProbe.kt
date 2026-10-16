package com.fixture.compat

import android.app.Application
import io.jankhunter.runtime.JankHunterScenario
import io.jankhunter.okhttp3.JankHunterOkHttp3
import kotlin.coroutines.Continuation
import kotlin.coroutines.EmptyCoroutineContext
import kotlin.coroutines.resume
import kotlin.coroutines.startCoroutine
import kotlin.coroutines.suspendCoroutine
import kotlin.reflect.full.memberFunctions

class FixtureApplication : Application() {
    override fun onCreate() { super.onCreate() }
}

@Retention(AnnotationRetention.RUNTIME)
annotation class Label(val value: String)

@Label("fixture")
class Reflected {
    fun defaultArgument(value: Int = 41): Int = value + 1
}

@JvmInline
value class Value(val number: Int)

private class OptimizedHelper { fun typed(value: Value) = value.number + 1 }

object CompatibilityProbe {
    @JvmStatic fun verify(): String {
        val scenario = JankHunterScenario.create("compatibility", "1")
        check(scenario.trace { 42 } == 42)
        val original = IllegalStateException()
        var threw = false
        try { scenario.trace { throw original } } catch (caught: Throwable) { threw = true; check(caught === original) }
        check(threw)
        val instance = Reflected()
        val method = Reflected::class.memberFunctions.single { it.name == "defaultArgument" }
        check(method.callBy(mapOf(method.parameters.first() to instance)) == 42)
        check(requireNotNull(Reflected::class.java.getAnnotation(Label::class.java)).value == "fixture")
        val dynamic = (System.nanoTime() and 31L).toInt()
        check(OptimizedHelper().typed(Value(dynamic)) == dynamic + 1)
        check(firstPart() + secondPart() == 42)
        var resumed = 0
        var suspended: Continuation<Int>? = null
        val block: suspend () -> Int = { suspendCoroutine { suspended = it } }
        block.startCoroutine(object : Continuation<Int> {
            override val context = EmptyCoroutineContext
            override fun resumeWith(result: Result<Int>) { resumed = result.getOrThrow() }
        })
        check(resumed == 0 && suspended != null)
        checkNotNull(suspended).resume(42)
        check(resumed == 42)
        check(JankHunterOkHttp3::class.java.getAnnotation(Metadata::class.java) != null)
        return "compatibility-ok"
    }
}
