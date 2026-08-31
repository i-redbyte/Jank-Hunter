package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RetainedLifecycleClassifierTest {
    @Test
    fun fragmentDestroyViewWatchesBindingFieldsInsteadOfFragmentInstance() {
        val fragment = CheckoutFragment()

        val targets = RetainedLifecycleClassifier.targets(fragment, "onDestroyView", "CheckoutOwner")

        assertEquals(1, targets.size)
        val target = targets.single()
        assertTrue(target.instance is CheckoutBinding)
        assertEquals(CheckoutBinding::class.java.name, target.description)
        assertEquals("CheckoutOwner", target.ownerHint)
    }

    @Test
    fun viewModelClearedWatchesViewModelItself() {
        val viewModel = CheckoutViewModel()

        val targets = RetainedLifecycleClassifier.targets(viewModel, "onCleared", null)

        assertEquals(1, targets.size)
        val target = targets.single()
        assertTrue(target.instance === viewModel)
        assertEquals(CheckoutViewModel::class.java.name, target.description)
        assertTrue(target.ownerHint.startsWith("lifecycle.onCleared."))
    }

    @Test
    fun recyclerViewHolderRecycleIsNotAValidRetentionBoundary() {
        val viewHolder = CheckoutViewHolder()

        val targets = RetainedLifecycleClassifier.targets(viewHolder, "onViewRecycled", null)

        assertTrue(targets.isEmpty())
    }

    @Test
    fun recyclerAdapterDetachIsNotAValidRetentionBoundary() {
        val adapter = CheckoutAdapter()

        val targets = RetainedLifecycleClassifier.targets(adapter, "onDetachedFromRecyclerView", null)

        assertTrue(targets.isEmpty())
    }

    @Test
    fun fragmentDestroyViewWatchesEveryAssociatedBindingField() {
        val fragment = LargeFragment()
        assertEquals(10, fragment.reachableBindings().size)

        val targets = RetainedLifecycleClassifier.targets(fragment, "onDestroyView", null)

        assertEquals(10, targets.size)
    }

    private class CheckoutFragment {
        @Suppress("unused")
        private val binding = CheckoutBinding()
    }

    private class CheckoutBinding

    private class LargeFragment {
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

    private class CheckoutViewModel

    private class CheckoutViewHolder {
        @Suppress("unused")
        private val binding = CheckoutBinding()
    }

    private class CheckoutAdapter
}
