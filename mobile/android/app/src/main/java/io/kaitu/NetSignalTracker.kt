package io.kaitu

import java.util.concurrent.atomic.AtomicBoolean

/**
 * Decides which NetEvent signal the next onAvailable should carry.
 *
 * The engine's netCoordinator ignores "changed" while it considers the network
 * down (after an "unavailable"), and nothing else on Android ever sends
 * "available". So after a full loss the first onAvailable must be "available";
 * every later one (e.g. WiFi->cellular handoff without onLost) stays "changed".
 */
class NetSignalTracker {
    private val armed = AtomicBoolean(false)

    /** Record the signal computed in onLost; only "unavailable" arms the tracker. */
    fun onLostSignal(signal: String) {
        if (signal == "unavailable") armed.set(true)
    }

    /** "available" exactly once after an "unavailable", otherwise "changed". */
    fun signalForAvailable(): String = if (armed.getAndSet(false)) "available" else "changed"

    fun reset() = armed.set(false)
}
