"use client";

import Script from "next/script";

export default function ChatwootWidget() {
  return (
    <Script
      id="chatwoot-sdk"
      strategy="afterInteractive"
      src="https://chat.anc.52j.me/packs/js/sdk.js"
      onLoad={() => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        (window as any).chatwootSDK.run({
          websiteToken: 'ZfFNvQRuoKzkik6X4KCSgp1h',
          baseUrl: 'https://chat.anc.52j.me'
        });
      }}
    />
  );
}
