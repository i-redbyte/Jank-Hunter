package com.fixture.feature;

public final class FeatureThreadOrigin {
    public void start() {
        new Thread(() -> { }).start();
    }
}
