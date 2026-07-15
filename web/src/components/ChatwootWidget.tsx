"use client";

import { useEffect, useState } from "react";
import Script from "next/script";
import { useSearchParams } from "next/navigation";
import { siteBrand } from "@/lib/brands";

/**
 * Hides Chatwoot widget DOM elements injected by the SDK.
 * The SDK injects its own DOM outside React's control, so we
 * must hide them directly when entering embed mode.
 */
function hideChatwootElements() {
  const selectors = ['.woot-widget-holder', '.woot-widget-bubble', '.woot--bubble-holder'];
  for (const sel of selectors) {
    const el = document.querySelector(sel) as HTMLElement | null;
    if (el) el.style.display = 'none';
  }
}

function showChatwootElements() {
  const selectors = ['.woot-widget-holder', '.woot-widget-bubble', '.woot--bubble-holder'];
  for (const sel of selectors) {
    const el = document.querySelector(sel) as HTMLElement | null;
    if (el) el.style.display = '';
  }
}

export default function ChatwootWidget() {
  const searchParams = useSearchParams();
  const [shouldLoad, setShouldLoad] = useState(false);
  // '' disables the support widget for this brand (overleap has no inbox yet).
  const chatwootToken = siteBrand().chatwootToken;

  const isEmbed = searchParams.get('embed') === 'true' ||
    (typeof window !== 'undefined' && window.location.hash === '#embed');

  // Reactively hide/show Chatwoot when embed state changes
  useEffect(() => {
    if (isEmbed) {
      hideChatwootElements();
    } else {
      showChatwootElements();
    }
  }, [isEmbed]);

  // Decide whether to load the SDK (only once, only if not embedded)
  useEffect(() => {
    if (!isEmbed) {
      setShouldLoad(true);
    }
  }, [isEmbed]);

  if (!shouldLoad || isEmbed || !chatwootToken) {
    return null;
  }

  return (
    <Script
      id="chatwoot-sdk"
      strategy="afterInteractive"
      src="https://chat.anc.52j.me/packs/js/sdk.js"
      onLoad={() => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (window as any).chatwootSDK.run({
          websiteToken: chatwootToken,
          baseUrl: 'https://chat.anc.52j.me'
        });
      }}
    />
  );
}
