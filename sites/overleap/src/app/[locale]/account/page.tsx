import { Suspense } from 'react';
import SubscriptionPanel from './SubscriptionPanel';

export default function AccountPage() {
  return (
    <Suspense>
      <SubscriptionPanel />
    </Suspense>
  );
}
