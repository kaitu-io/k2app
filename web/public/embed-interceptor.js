/**
 * Embed-mode link interceptor — loaded with next/script `beforeInteractive`
 * from the [locale] layout so it is active BEFORE React hydration. In embed
 * mode (app webview iframe) no navigation may ever happen inside the iframe:
 * every link click is forwarded to the parent app via postMessage, which opens
 * it in the system browser. React-side interception (formerly in useEmbedMode)
 * attached only after hydration, leaving a window where clicks hit the native
 * webview's default new-window behaviour.
 *
 * Served from the live site, so it also protects already-shipped app versions
 * that embed these pages without an iframe sandbox.
 *
 * The postMessage call must stay wrapped in try/catch — sentry-filters.ts
 * (dropNativePostMessageRejections) relies on app-originated postMessage never
 * producing an unhandled rejection.
 */
(function () {
  'use strict';
  try {
    var isEmbedded =
      /[?&]embed=true(&|$)/.test(window.location.search) ||
      window.location.hash === '#embed';
    // Not embedded, or opened as a top-level document: normal navigation.
    if (!isEmbedded || window.parent === window) return;

    var send = function (url) {
      try {
        window.parent.postMessage(
          { type: 'external-link', url: url, timestamp: Date.now() },
          // Parent origin (tauri://localhost, capacitor://, …) is unknowable
          // from inside the iframe; the receiving side validates event.origin.
          '*'
        );
      } catch (e) {
        /* worst case is a dead click — never navigate the iframe */
      }
    };

    var handleClick = function (e) {
      var target = e.target;
      var link = target && target.closest ? target.closest('a') : null;
      if (!link || !link.href) return;

      var url;
      try {
        url = new URL(link.href);
      } catch (err) {
        e.preventDefault();
        e.stopPropagation();
        return;
      }

      // Same-document hash jumps are harmless — let them through.
      if (
        url.origin === window.location.origin &&
        url.pathname === window.location.pathname &&
        url.hash
      ) {
        return;
      }

      e.preventDefault();
      e.stopPropagation();

      if (url.protocol !== 'http:' && url.protocol !== 'https:') return;
      send(url.toString());
    };

    document.addEventListener('click', handleClick, true);
    document.addEventListener('auxclick', handleClick, true);

    // Cover programmatic opens (window.open) the click handler can't see.
    window.open = function (u) {
      try {
        var url = new URL(u, window.location.href);
        if (url.protocol === 'http:' || url.protocol === 'https:') {
          send(url.toString());
        }
      } catch (e) {
        /* invalid URL — swallow */
      }
      return null;
    };
  } catch (e) {
    /* interceptor must never break the page */
  }
})();
