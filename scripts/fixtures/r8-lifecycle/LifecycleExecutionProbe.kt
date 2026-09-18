package com.example.jhsmoke

import android.content.Intent
import android.os.IBinder
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import androidx.fragment.app.Fragment
import androidx.fragment.app.FragmentActivity
import androidx.fragment.app.FragmentFactory
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelStore
import com.example.jhsmoke.databinding.ProbeBinding
import com.example.jhsmoke.feature.LibraryBindingFragment
import io.jankhunter.annotations.JankHunterOwner
import io.jankhunter.annotations.JankHunterIgnore
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterLifecycleAccessorV1
import io.jankhunter.runtime.JankHunterBindingAccessor
import io.jankhunter.runtime.JankHunterLifecycleTargetSinkV1
import kotlin.properties.ReadOnlyProperty
import kotlin.reflect.KProperty
import java.io.File

/** The fixture deliberately retains targets. Production registration must only be weak. */
object LifecycleExecutionProbe {
    private val held = ArrayList<Any>()
    private var unexpectedInitializations = 0

    @JvmStatic
    fun run(activity: FragmentActivity) {
        check(JankHunter.reconfigure("lifecycle-probe") {
            it.retainedObjectDelayMs(1000).retainedObjectForceGcEnabled(true).retainedHeapDumpEnabled(false)
                .logDirectory(File(activity.filesDir, "lifecycle-probe-${android.os.Process.myPid()}"))
        })
        val manager = activity.supportFragmentManager
        verifyLegacyCallbacks(activity)
        manager.fragmentFactory = object : FragmentFactory() {
            override fun instantiate(classLoader: ClassLoader, className: String): Fragment = Child()
        }
        val fragment = manager.fragmentFactory.instantiate(activity.classLoader, Child::class.java.name)
        expected("probe.binding", "fragment", fragment)
        held.add(fragment)
        manager.beginTransaction().add(android.R.id.content, fragment).commitNow()
        manager.beginTransaction().remove(fragment).commitNow()
        check(unexpectedInitializations == 0) { "Uninitialized Lazy was evaluated" }

        val custom = CustomDelegateFragment()
        expected("probe.custom", "fragment", custom)
        held.add(custom)
        manager.beginTransaction().add(android.R.id.content, custom).commitNow()
        manager.beginTransaction().remove(custom).commitNow()

        val platform = Platform()
        expected("probe.platform", "fragment", platform)
        held.add(platform)
        activity.fragmentManager.beginTransaction().add(android.R.id.content, platform).commit()
        activity.fragmentManager.executePendingTransactions()

        val library = FromLibrary()
        expected("probe.library", "fragment", library)
        held.add(library)
        activity.fragmentManager.beginTransaction().add(android.R.id.content, library).commit()
        activity.fragmentManager.executePendingTransactions()
        activity.fragmentManager.beginTransaction().remove(library).commit()
        activity.fragmentManager.executePendingTransactions()
        activity.fragmentManager.beginTransaction().remove(platform).commit()
        activity.fragmentManager.executePendingTransactions()

        activity.startActivity(Intent(activity, LifecycleActivity::class.java))
        activity.startService(Intent(activity, LifecycleService::class.java))

        val store = ViewModelStore()
        val ordinary = Model()
        val finalCallback = FinalModel()
        expected("probe.vm", "viewmodel", ordinary)
        expected("probe.final", "viewmodel", finalCallback)
        held.add(ordinary)
        held.add(finalCallback)
        store.put("ordinary", ordinary)
        store.put("final", finalCallback)
        store.clear()
        Handler(Looper.getMainLooper()).postDelayed({
            val archive = File(activity.getExternalFilesDir(null), "lifecycle-execution-${android.os.Process.myPid()}.zip")
            check(JankHunter.captureLogArchiveAsync(archive) { result ->
                if (result == null) Log.e("JHLIFECYCLER8", "EXECUTION FAIL archive capture returned null")
                else Log.i("JHLIFECYCLER8", "EXECUTION PASS held=${held.size} lazy=$unexpectedInitializations archive=$archive")
            })
        }, 4000)
    }

    private fun verifyLegacyCallbacks(activity: FragmentActivity) {
        val fragment: Fragment = LegacyFragment()
        val model: ViewModel = LegacyModel()
        check(fragment !is JankHunterLifecycleAccessorV1 && model !is JankHunterLifecycleAccessorV1)
        held.add(fragment)
        held.add(model)
        val manager = activity.supportFragmentManager
        manager.beginTransaction().add(android.R.id.content, fragment).commitNow()
        val view = checkNotNull(fragment.view)
        held.add(view)
        expected("probe.legacy.fragment", "fragment", fragment)
        expected("probe.legacy.fragment", "root", view)
        expected("probe.legacy.vm", "viewmodel", model)
        LegacyLifecycleCaller.watch(fragment, "onDestroyView", "probe.legacy.fragment")
        LegacyLifecycleCaller.watch(fragment, "onDestroy", "probe.legacy.fragment")
        LegacyLifecycleCaller.watch(model, "onCleared", "probe.legacy.vm")
        manager.beginTransaction().remove(fragment).commitNow()
    }

    @JankHunterIgnore
    class LegacyFragment : Fragment() {
        override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, state: Bundle?): View =
            View(inflater.context)
    }

    @JankHunterIgnore
    class LegacyModel : ViewModel()

    private fun expected(owner: String, role: String, instance: Any, holder: String = owner) {
        Log.i("JHLIFECYCLER8", "EXPECTED owner=$owner role=$role class=${instance.javaClass.name} holder=$holder")
    }

    @JankHunterOwner("probe.binding")
    open class BindingBase<T> : Fragment() {
        private var erased: T? = null
        private var initialized: Lazy<ProbeBinding>? = null
        private val pending: ProbeBinding by lazy { unexpectedInitializations++; error("must not initialize") }
        private val fake = FakeBinding()

        override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, state: Bundle?): View {
            val binding = ProbeBinding.inflate(inflater, container, false)
            expected("probe.binding", "binding", binding)
            expected("probe.binding", "root", binding.root)
            held.add(binding)
            held.add(binding.root)
            // Keep the generic field genuinely erased in bytecode without an unchecked cast.
            install(binding)
            initialized = lazyOf(binding)
            held.add(fake)
            check(fake !== binding as Any)
            return binding.root
        }

        open fun install(binding: ProbeBinding) = Unit
        protected fun store(value: T) { erased = value }

        override fun onDestroyView() {
            erased = null
            initialized = null
            super.onDestroyView()
        }
    }

    @JankHunterOwner("probe.binding")
    class Child : BindingBase<ProbeBinding>() {
        override fun install(binding: ProbeBinding) = store(binding)
    }

    private class ExistingBindingDelegate : ReadOnlyProperty<Any?, ProbeBinding> {
        var existing: ProbeBinding? = null
        override fun getValue(thisRef: Any?, property: KProperty<*>): ProbeBinding =
            error("Custom delegate getter must never be invoked by observation")
    }

    @JankHunterOwner("probe.custom")
    class CustomDelegateFragment : Fragment(), JankHunterBindingAccessor {
        private val delegate = ExistingBindingDelegate()
        private val custom by delegate

        override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, state: Bundle?): View {
            val binding = ProbeBinding.inflate(inflater, container, false)
            delegate.existing = binding
            expected("probe.custom", "binding", binding)
            expected("probe.custom", "root", binding.root)
            held.add(binding)
            held.add(binding.root)
            return binding.root
        }

        override fun visitJankHunterBindings(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) {
            delegate.existing?.let { binding ->
                sink.accept(binding, ownerHint)
                sink.accept(binding.root, ownerHint)
            }
        }

        override fun onDestroyView() {
            delegate.existing = null
            super.onDestroyView()
        }
    }

    @JankHunterOwner("probe.platform")
    class Platform : android.app.Fragment() {
        private var binding: ProbeBinding? = null
        override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, state: Bundle?): View {
            val value = ProbeBinding.inflate(inflater, container, false)
            expected("probe.platform", "binding", value)
            expected("probe.platform", "root", value.root)
            binding = value
            held.add(value)
            held.add(value.root)
            return value.root
        }
        override fun onDestroyView() { binding = null; super.onDestroyView() }
    }

    @JankHunterOwner("probe.vm")
    class Model : ViewModel()

    @JankHunterOwner("probe.final")
    open class FinalBase : ViewModel() {
        final override fun onCleared() { super.onCleared() }
    }

    @JankHunterOwner("probe.final")
    class FinalModel : FinalBase()

    @JankHunterOwner("probe.activity")
    class LifecycleActivity : android.app.Activity() {
        override fun onCreate(state: Bundle?) {
            super.onCreate(state)
            expected("probe.activity", "activity", this, "lifecycle.destroyed.${componentName.className}")
            expected("probe.activity", "decor", window.decorView)
            held.add(window.decorView)
            held.add(this)
            finish()
        }
    }

    @JankHunterOwner("probe.service")
    class LifecycleService : android.app.Service() {
        override fun onCreate() {
            super.onCreate()
            expected("probe.service", "service", this)
            held.add(this)
        }
        override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
            stopSelf(startId)
            return START_NOT_STICKY
        }
        override fun onBind(intent: Intent?): IBinder? = null
    }

    private class FakeBinding

    @JankHunterOwner("probe.library")
    class FromLibrary : LibraryBindingFragment()
}
