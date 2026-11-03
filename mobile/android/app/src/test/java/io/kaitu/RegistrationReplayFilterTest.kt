package io.kaitu

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the decision behind K2VpnService.registerNetworkCallback: Android
 * replays onAvailable for every already-satisfying network right after
 * registerNetworkCallback(), and that replay must not be forwarded to the
 * engine as a network change (field log: 33/33 connects followed ~0.55s later
 * by a scoreboard-wiping "engine: network reconnect"). Anything that could be
 * a real change must still pass.
 *
 * Plain strings stand in for android.net.Network — the filter only uses ==.
 */
class RegistrationReplayFilterTest {

    private val t0 = 1_000_000L

    @Test
    fun replayOfEngineStartNetwork_isSkipped() {
        val f = RegistrationReplayFilter.create("cell-100", "cell-100", t0)
        assertTrue(f.shouldSkip("cell-100", t0 + 11))
    }

    @Test
    fun replayIsSkippedOnlyOnce() {
        val f = RegistrationReplayFilter.create("cell-100", "cell-100", t0)
        assertTrue(f.shouldSkip("cell-100", t0 + 11))
        assertFalse(f.shouldSkip("cell-100", t0 + 20))
    }

    @Test
    fun differentNetwork_isNotSkipped() {
        val f = RegistrationReplayFilter.create("cell-100", "cell-100", t0)
        assertFalse(f.shouldSkip("wifi-101", t0 + 11))
        // The baseline replay that follows is still recognised.
        assertTrue(f.shouldSkip("cell-100", t0 + 12))
    }

    @Test
    fun defaultNetworkChangedDuringEngineStart_nothingSkipped() {
        // Engine dialed over cell-100, but by registration the default is
        // wifi-101: the engine's sockets are on the old path, so the replay
        // for wifi-101 IS a real change and must reach the engine.
        val f = RegistrationReplayFilter.create("cell-100", "wifi-101", t0)
        assertFalse(f.shouldSkip("wifi-101", t0 + 11))
        assertFalse(f.shouldSkip("cell-100", t0 + 11))
    }

    @Test
    fun unknownNetworkAtStartOrRegistration_nothingSkipped() {
        assertFalse(RegistrationReplayFilter.create<String>(null, "cell-100", t0).shouldSkip("cell-100", t0 + 11))
        assertFalse(RegistrationReplayFilter.create("cell-100", null, t0).shouldSkip("cell-100", t0 + 11))
        assertFalse(RegistrationReplayFilter.create<String>(null, null, t0).shouldSkip("cell-100", t0 + 11))
    }

    @Test
    fun lateOnAvailableForBaseline_isNotSkipped() {
        val f = RegistrationReplayFilter.create("cell-100", "cell-100", t0)
        assertFalse(f.shouldSkip("cell-100", t0 + RegistrationReplayFilter.REPLAY_WINDOW_MS + 1))
    }

    @Test
    fun baselineLostBeforeReplay_laterOnAvailableIsNotSkipped() {
        val f = RegistrationReplayFilter.create("cell-100", "cell-100", t0)
        f.onLost("cell-100")
        assertFalse(f.shouldSkip("cell-100", t0 + 50))
    }

    @Test
    fun lostOfOtherNetwork_keepsBaseline() {
        val f = RegistrationReplayFilter.create("cell-100", "cell-100", t0)
        f.onLost("wifi-101")
        assertTrue(f.shouldSkip("cell-100", t0 + 50))
    }
}
