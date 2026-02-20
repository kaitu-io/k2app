import React from "react";

// Root layout is a pass-through to allow route groups to define their own html/body.
// This is required for Payload CMS which provides its own RootLayout with html/body.
// Each route group (cms), (manager), [locale] defines its own complete layout.
export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return children;
}
