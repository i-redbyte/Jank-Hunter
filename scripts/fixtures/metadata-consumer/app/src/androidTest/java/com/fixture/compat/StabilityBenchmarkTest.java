package com.fixture.compat;

import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.junit.Test;
import android.app.Activity;
import android.content.Intent;
import org.junit.runner.RunWith;
import static org.junit.Assert.assertEquals;

@RunWith(AndroidJUnit4.class)
public class StabilityBenchmarkTest {
    @Test public void pacedMainThreadCollectorWorkload() {
        String mode = InstrumentationRegistry.getArguments().getString("mode");
        int seconds = Integer.parseInt(InstrumentationRegistry.getArguments().getString("seconds"));
        android.app.Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
        android.content.Context target = instrumentation.getTargetContext();
        Activity activity = instrumentation.startActivitySync(new Intent().setClassName(target, "com.fixture.compat.BenchmarkActivity").addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
        try { assertEquals("benchmark-ok", StabilityBenchmark.run(activity, mode, seconds)); }
        finally { instrumentation.runOnMainSync(() -> activity.finish()); }
    }
}
