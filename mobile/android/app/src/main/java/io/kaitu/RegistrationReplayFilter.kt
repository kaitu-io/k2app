package io.kaitu

/**
 * Recognises the onAvailable that ConnectivityManager.registerNetworkCallback()
 * replays for a network that ALREADY satisfies the request at registration
 * time, so K2VpnService does not forward it to the engine as a network change.
 *
 * Why: K2VpnService registers its callback right after engine.start() returns.
 * Android then immediately calls onAvailable for the current network (field
 * log: 192/194 engine starts followed by onAvailable 2-83 ms later, median
 * 11 ms). That was sent as NetEvent "changed" and, 500 ms later, ran the
 * engine's scoreboard-wiping doNetworkReconnect — on every connect, killing
 * the first in-flight DNS/TCP dials and redialing a transport that had just
 * been established on the very same network.
 *
 * Only the narrowest case is skipped, once:
 *   - the network that was the default when the engine started dialing
 *     (networkAtStart) is still the default at registration
 *     (networkAtRegistration) — otherwise the engine's sockets may sit on a
 *     path that is already gone, and the replay is a real change;
 *   - the onAvailable is for exactly that network;
 *   - it arrives within REPLAY_WINDOW_MS of registration;
 *   - that network has not been reported lost in between.
 * Every other onAvailable (another network, a later one, the first after an
 * onLost) passes through unchanged.
 *
 * Generic over the network handle so it can be unit-tested on the plain JVM
 * (android.net.Network compares by netId via equals()). Created before
 * registration, then touched only from the NetworkCallback thread.
 */
internal class RegistrationReplayFilter<N : Any> private constructor(
    private var baseline: N?,
    private val registeredAtMs: Long,
) {
    /** True if this onAvailable is the registration replay for the engine's start network. Consumes it. */
    fun shouldSkip(network: N, nowMs: Long): Boolean {
        val b = baseline ?: return false
        if (network != b) return false
        baseline = null // one-shot: any later onAvailable for it is real
        return nowMs - registeredAtMs <= REPLAY_WINDOW_MS
    }

    fun onLost(network: N) {
        if (network == baseline) baseline = null
    }

    companion object {
        /** Replay arrives within ~100 ms in the field; 2 s bounds a slow main looper. */
        const val REPLAY_WINDOW_MS = 2_000L

        fun <N : Any> create(networkAtStart: N?, networkAtRegistration: N?, registeredAtMs: Long): RegistrationReplayFilter<N> {
            val baseline = if (networkAtStart != null && networkAtStart == networkAtRegistration) networkAtStart else null
            return RegistrationReplayFilter(baseline, registeredAtMs)
        }
    }
}
