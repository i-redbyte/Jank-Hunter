package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterLifecycleAccessorV1
import io.jankhunter.runtime.JankHunterLifecycleTargetSinkV1
import io.jankhunter.runtime.JankHunterBindingAccessor
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RetainedLifecycleClassifierTest {
    private data class Target(val instance: Any, val description: String, val ownerHint: String)
    // Test adapter collects unique emissions; production delegates deduplication to the bounded watcher.
    private fun collect(visit: (JankHunterLifecycleTargetSinkV1) -> Unit): List<Target> = buildList {
        visit(JankHunterLifecycleTargetSinkV1 { value, owner ->
            if (value != null && none { it.instance === value }) add(Target(value, value.javaClass.name, checkNotNull(owner)))
        })
    }
    private fun targets(instance: Any?, event: String?, owner: String?) = collect {
        RetainedLifecycleClassifier.visitTargets(instance, event, owner, it)
    }
    private fun typedTargets(instance: Any?, kind: Int, event: String?, owner: String?) = collect {
        RetainedLifecycleClassifier.visitTypedTargets(instance, kind, event, owner, it)
    }

    @Test
    fun failingExplicitAccessorDoesNotDiscardAlreadyCapturedAutomaticTargets() {
        val binding = Any()
        val instance = object : JankHunterLifecycleAccessorV1, JankHunterBindingAccessor {
            override fun jankHunterLifecycleKindV1(): Int = 2
            override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) {
                sink.accept(binding, ownerHint)
            }
            override fun visitJankHunterBindings(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) {
                throw IllegalStateException("custom accessor failure")
            }
        }
        val targets = typedTargets(instance, 2, "onDestroyView", "owner")
        assertEquals(1, targets.size)
        assertTrue(targets.single().instance === binding)
    }

    @Test
    fun legacyEntryPointUsesGeneratedKindWithoutRuntimeClassName() {
        val instance = Renamed(3)
        val targets = targets(instance, "onCleared", "owner")
        assertEquals(1, targets.size)
        assertTrue(targets.single().instance === instance)
    }

    @Test
    fun helperAccessorDoesNotEnableWatchingAnExcludedInstance() {
        val instance = Renamed(0)
        assertTrue(typedTargets(instance, 3, "onCleared", "owner").isEmpty())
    }

    @Test
    fun classNamesDoNotMakeArbitraryObjectsLifecycleTypes() {
        assertTrue(targets(FakeFragment(), "onDestroy", null).isEmpty())
        assertTrue(targets(FakeViewModel(), "onCleared", null).isEmpty())
    }

    private class FakeFragment
    private class FakeViewModel

    private class Renamed(private val kind: Int) : JankHunterLifecycleAccessorV1 {
        override fun jankHunterLifecycleKindV1(): Int = kind
        override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) = Unit
    }

    @Test
    fun reflectionMetadataUsesBoundedWeakClassCache() {
        assertTrue(
            RetainedLifecycleClassifier::class.java.declaredFields.any {
                it.type.simpleName == "BoundedWeakIdentityCache"
            },
        )
    }

    @Test
    fun fragmentDestroyViewWatchesBindingFieldsInsteadOfFragmentInstance() {
        val fragment = CheckoutFragment()

        val targets = targets(fragment, "onDestroyView", "CheckoutOwner")

        assertEquals(1, targets.size)
        val target = targets.single()
        assertTrue(target.instance is CheckoutBinding)
        assertEquals(CheckoutBinding::class.java.name, target.description)
        assertEquals("CheckoutOwner", target.ownerHint)
    }

    @Test
    fun viewModelClearedWatchesViewModelItself() {
        val viewModel = CheckoutViewModel()

        val targets = targets(viewModel, "onCleared", null)

        assertEquals(1, targets.size)
        val target = targets.single()
        assertTrue(target.instance === viewModel)
        assertEquals(CheckoutViewModel::class.java.name, target.description)
        assertTrue(target.ownerHint.startsWith("lifecycle.onCleared."))
    }

    @Test
    fun recyclerViewHolderRecycleIsNotAValidRetentionBoundary() {
        val viewHolder = CheckoutViewHolder()

        val targets = targets(viewHolder, "onViewRecycled", null)

        assertTrue(targets.isEmpty())
    }

    @Test
    fun recyclerAdapterDetachIsNotAValidRetentionBoundary() {
        val adapter = CheckoutAdapter()

        val targets = targets(adapter, "onDetachedFromRecyclerView", null)

        assertTrue(targets.isEmpty())
    }

    @Test
    fun fragmentDestroyViewWatchesEveryAssociatedBindingField() {
        val fragment = LargeFragment()
        assertEquals(10, fragment.reachableBindings().size)

        val targets = targets(fragment, "onDestroyView", null)

        assertEquals(10, targets.size)
    }

    private class CheckoutFragment : JankHunterLifecycleAccessorV1 {
        private val binding = CheckoutBinding()
        override fun jankHunterLifecycleKindV1(): Int = 2
        override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) {
            sink.accept(binding, ownerHint)
        }
    }

    private class CheckoutBinding

    private class LargeFragment : JankHunterLifecycleAccessorV1 {
        private val binding0 = CheckoutBinding()
        private val binding1 = CheckoutBinding()
        private val binding2 = CheckoutBinding()
        private val binding3 = CheckoutBinding()
        private val binding4 = CheckoutBinding()
        private val binding5 = CheckoutBinding()
        private val binding6 = CheckoutBinding()
        private val binding7 = CheckoutBinding()
        private val binding8 = CheckoutBinding()
        private val binding9 = CheckoutBinding()

        override fun jankHunterLifecycleKindV1(): Int = 2
        override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) {
            reachableBindings().forEach { sink.accept(it, ownerHint) }
        }

        fun reachableBindings(): List<CheckoutBinding> {
            return listOf(
                binding0,
                binding1,
                binding2,
                binding3,
                binding4,
                binding5,
                binding6,
                binding7,
                binding8,
                binding9,
            )
        }
    }

    private class CheckoutViewModel : JankHunterLifecycleAccessorV1 {
        override fun jankHunterLifecycleKindV1(): Int = 3
        override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) = Unit
    }

    private class CheckoutViewHolder {
        @Suppress("unused")
        private val binding = CheckoutBinding()
    }

    private class CheckoutAdapter
}
