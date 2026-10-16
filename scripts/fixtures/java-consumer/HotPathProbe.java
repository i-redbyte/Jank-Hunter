package com.fixture.compat;

import android.content.Context;
import com.fixture.callback.CallbackPipeline;
import io.jankhunter.runtime.JankHunter;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;

public final class HotPathProbe {
    public static String verify(Context context) throws Exception {
        int[] counts = new int[6];
        CallbackPipeline pipeline = HookFixture.create(() -> counts[4]++, () -> counts[5]++, () -> {
            throw new AssertionError("callback failure");
        });
        for (int index = 0; index < 64; index++) pipeline.logDebug(() -> {
            if (!pipeline.holdsMonitor()) throw new AssertionError("pre-init monitor");
            counts[0]++;
            return "fixture";
        }, "fixture");
        pipeline.initialize(() -> { if (pipeline.holdsMonitor()) counts[2]++; else counts[3]++; });
        for (int index = 0; index < 1000; index++) pipeline.logDebug(() -> {
            if (pipeline.holdsMonitor()) throw new AssertionError("ready monitor");
            counts[1]++;
            return "fixture";
        }, "fixture");
        if (counts[0] != 64 || counts[1] != 1000 || counts[2] != 64 || counts[3] != 1000 ||
            counts[4] != 1064 || counts[5] != 0) throw new AssertionError("callback observations");
        JankHunter.flush();
        File folder = context.getExternalFilesDir(null);
        if (folder == null) throw new AssertionError("fixture storage");
        File archive = new File(folder, "compat-hot-path.zip");
        if (JankHunter.captureLogArchive(archive) == null || archive.length() == 0) throw new AssertionError("empty archive");
        String json = "{\"pre_init\":64,\"ready\":1000,\"masked_inside\":64,\"masked_outside\":1000," +
            "\"handled\":1064,\"dropped\":0,\"failures\":0}\n";
        try (FileOutputStream output = new FileOutputStream(new File(folder, "hot-path-counts.json"))) {
            output.write(json.getBytes(StandardCharsets.UTF_8));
        }
        return "hot-path-ok";
    }
}
