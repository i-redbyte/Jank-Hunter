package io.jankhunter.runtime

/** Outcome of a process-local [JankHunter.switchBinaryStorage] request. */
enum class JankHunterStorageSwitchResult {
    /** Existing sealed segments were consolidated and new events now use the requested storage. */
    SWITCHED,

    /** The exact requested storage instance is already active. */
    ALREADY_ACTIVE,

    /** Jank Hunter has no initialized configuration to which the storage can be bound. */
    NOT_STARTED,

    /** Runtime shutdown or a terminal writer failure closed admission before the switch. */
    NOT_ACCEPTING,

    /** A best-effort runtime did not reach the ordered switch frontier before its deadline. */
    TIMED_OUT,

    /** Target preparation failed and Jank Hunter kept or restored the previous storage. */
    FAILED,
}
