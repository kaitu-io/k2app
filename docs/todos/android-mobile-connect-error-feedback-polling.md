Android mobile: connect() error feedback relies solely on polling

K2Plugin.connect() resolves immediately (fire-and-forget) before VPN is established. Errors are only surfaced via 2s status polling + 5s optimistic timeout. Consider dynamic optimistic timeout (reset when poll returns "connecting"). This is a secondary concern — root cause is the error state UX gap (see android-mobile-error-state-ux-gap.md).

Scrum verdict: Author ✓ — keep as-is, improve as part of error state fix. From Issue #7.
