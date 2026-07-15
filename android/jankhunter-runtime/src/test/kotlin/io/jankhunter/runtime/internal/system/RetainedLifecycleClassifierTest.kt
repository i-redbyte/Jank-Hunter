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
        assertEquals("lifecycle.autowatch.fragment_binding", target.flow)
        assertEquals("onDestroyView", target.step)
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
        assertEquals("lifecycle.autowatch.viewmodel", target.flow)
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

    private class CheckoutFragment {
        @Suppress("unused")
        private val binding = CheckoutBinding()
    }

    private class CheckoutBinding

    private class CheckoutViewModel

    private class CheckoutViewHolder {
        @Suppress("unused")
        private val binding = CheckoutBinding()
    }

    private class CheckoutAdapter
}
