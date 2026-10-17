package com.fixture.compat;

import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.junit.Test;
import org.junit.runner.RunWith;
import static org.junit.Assert.assertEquals;

@RunWith(AndroidJUnit4.class)
public class RegistryFootprintTest {
    @Test public void exactRegistryRetainedFootprint() {
        assertEquals("footprint-ok", RegistryFootprint.capture(InstrumentationRegistry.getInstrumentation().getTargetContext()));
    }
}
