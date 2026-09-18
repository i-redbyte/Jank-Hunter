package com.example.jhsmoke.feature

import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.util.Log
import com.example.jhsmoke.feature.databinding.FeatureProbeBinding
import io.jankhunter.annotations.JankHunterOwner

/** Compiled and instrumented in a separate library before the host's ALL pass. */
@JankHunterOwner("probe.library")
open class LibraryBindingFragment : android.app.Fragment() {
    private var binding: FeatureProbeBinding? = null

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, state: Bundle?): View {
        val value = FeatureProbeBinding.inflate(inflater, container, false)
        Log.i("JHLIFECYCLER8", "EXPECTED owner=probe.library role=binding class=${value.javaClass.name}")
        Log.i("JHLIFECYCLER8", "EXPECTED owner=probe.library role=root class=${value.root.javaClass.name}")
        binding = value
        held.add(value)
        held.add(value.root)
        return value.root
    }

    override fun onDestroyView() {
        binding = null
        super.onDestroyView()
    }

    companion object {
        private val held = ArrayList<Any>()
    }
}
