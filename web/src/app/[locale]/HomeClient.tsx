"use client";

import dynamic from 'next/dynamic';

/**
 * HomeClient — client-side interactive portion of the homepage.
 *
 * Extracted from the server component page.tsx to allow the main page
 * to be a Server Component (SSR/SSG) while keeping canvas-based
 * animations as a client-only dynamic import.
 */

// Dynamic import for Canvas component to avoid SSR issues
const MPTCPVisualization = dynamic(
  () => import('@/components/MPTCPVisualization'),
  { ssr: false }
);

/**
 * Renders the MPTCP visualization canvas animation.
 * Must be a client component because the canvas animation requires
 * browser APIs (requestAnimationFrame, HTMLCanvasElement, etc.).
 */
export default function HomeClient(): React.ReactElement {
  return <MPTCPVisualization />;
}
