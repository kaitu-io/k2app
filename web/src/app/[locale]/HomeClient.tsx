'use client';

import dynamic from 'next/dynamic';

const K2ccPulseCanvas = dynamic(
  () => import('@/components/k2cc-hero/K2ccPulseCanvas'),
  { ssr: false },
);

export default function HomeClient() {
  return <K2ccPulseCanvas />;
}
