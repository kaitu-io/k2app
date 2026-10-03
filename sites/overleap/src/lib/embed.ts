/**
 * Pages opened inside the app (`?embed=true` or `#embed`) hide site chrome and
 * extras such as the chat widget.
 */
export function isEmbeddedPage(): boolean {
  if (typeof window === 'undefined') return false;
  return new URLSearchParams(window.location.search).get('embed') === 'true' || window.location.hash === '#embed';
}
