package com.fixture.app;

import com.fixture.feature.FeatureThreadOrigin;

public final class AppThreadOrigin {
    public void startBoth() {
        new Thread(() -> { }).start();
        new FeatureThreadOrigin().start();
    }
}
