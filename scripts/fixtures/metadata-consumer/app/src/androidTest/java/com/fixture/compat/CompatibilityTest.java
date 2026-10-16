package com.fixture.compat;

import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.junit.Test;
import org.junit.runner.RunWith;
import static org.junit.Assert.assertEquals;

@RunWith(AndroidJUnit4.class)
public class CompatibilityTest {
    @Test public void minifiedTargetPreservesKotlinAndJankHunterContracts() {
        assertEquals("compatibility-ok", CompatibilityProbe.verify());
    }
    @Test public void fullInstrumentedCallbacksPreserveStartupAndReadyPaths() {
        assertEquals("hot-path-ok", HotPathProbe.verify(InstrumentationRegistry.getInstrumentation().getTargetContext()));
    }
}
