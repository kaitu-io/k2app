package io.kaitu

import org.junit.Assert.assertEquals
import org.junit.Test

class NetSignalTrackerTest {
    @Test fun tracker_available_after_unavailable() {
        val t = NetSignalTracker(); t.onLostSignal("unavailable")
        assertEquals("available", t.signalForAvailable())
    }

    @Test fun tracker_only_first_available_after_unavailable() {
        val t = NetSignalTracker(); t.onLostSignal("unavailable"); t.onLostSignal("unavailable")
        assertEquals("available", t.signalForAvailable())
        assertEquals("changed", t.signalForAvailable())
    }

    @Test fun tracker_changed_on_lost_does_not_arm() {
        val t = NetSignalTracker(); t.onLostSignal("changed")
        assertEquals("changed", t.signalForAvailable())
    }

    @Test fun tracker_default_is_changed() {
        assertEquals("changed", NetSignalTracker().signalForAvailable())
    }

    @Test fun tracker_reset_clears_arm() {
        val t = NetSignalTracker(); t.onLostSignal("unavailable"); t.reset()
        assertEquals("changed", t.signalForAvailable())
    }
}
