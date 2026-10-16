package com.fixture.app;

import com.fixture.feature.FeatureThreadOrigin;

public final class AppThreadOrigin {
    @javax.inject.Inject public FeatureThreadOrigin injected;
    public void startBoth() {
        new Thread(() -> { }).start();
        new FeatureThreadOrigin().start();
    }
}
