// Pass-through: [locale]/layout.tsx owns <html>/<body> so `lang` is per locale.
export default function RootLayout({ children }: { children: React.ReactNode }) {
  return children;
}
