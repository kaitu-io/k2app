import type { SxProps, Theme } from '@mui/material/styles';

/** Max width of the tunnel-list column in the desktop (sidebar) layout. */
export const TUNNEL_LIST_MAX_WIDTH = 760;

/**
 * Width constraint for Dashboard SECTION 2 (tunnel lists).
 * Desktop/sidebar layout: cap + centre so rows don't stretch across a 1040px
 * window. Mobile layout: untouched — the phone-shaped window is full-bleed.
 */
export function tunnelListSx(isDesktop: boolean): SxProps<Theme> {
  if (!isDesktop) return {};
  return { maxWidth: TUNNEL_LIST_MAX_WIDTH, width: '100%', mx: 'auto' };
}
