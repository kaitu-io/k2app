Android mobile: error state UX gap

Go engine only has 3 states (disconnected/connecting/connected), so mobile VPN never enters `error` or `reconnecting` state. When connection fails, user sees "disconnected" without error feedback.

Short-term: audit Dashboard to show error when `isDisconnected && status.error` is present.
Long-term: add Android NetworkCallback for auto-reconnect on network change.

Scrum verdict: Compromise, from Issue #4.
