package io.jankhunter.okhttp3;

/**
 * Versioned ABI for transport bytecode hooks. State belongs to the instrumented connection;
 * applications do not need to implement this interface or access its internal state.
 */
public interface JankHunterHttpTransportV1 {
    Object jankHunterTransportState();
    void jankHunterTransportState(Object state);
}
