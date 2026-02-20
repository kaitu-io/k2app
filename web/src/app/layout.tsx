import React from "react";

// Root layout is a pass-through — [locale] and (manager) route groups each define
// their own <html>/<body> with independent styling and providers.
export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return children;
}
