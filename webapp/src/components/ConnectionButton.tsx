import { cva, type VariantProps } from 'class-variance-authority';
import { useTranslation } from 'react-i18next';
import { cn } from '../lib/cn';
import type { VpnState } from '../vpn-client';

const buttonVariants = cva(
  'w-32 h-32 rounded-full text-white font-bold text-lg transition-all duration-300 shadow-lg',
  {
    variants: {
      vpnState: {
        stopped: 'bg-blue-600 hover:bg-blue-700 active:scale-95',
        connecting: 'bg-yellow-500 animate-pulse cursor-not-allowed',
        connected: 'bg-green-600 hover:bg-red-500',
      },
    },
    defaultVariants: {
      vpnState: 'stopped',
    },
  }
);

interface Props extends VariantProps<typeof buttonVariants> {
  state: VpnState;
  onConnect: () => void;
  onDisconnect: () => void;
  className?: string;
}

export function ConnectionButton({ state, onConnect, onDisconnect, className }: Props) {
  const { t } = useTranslation('dashboard');

  const isTransitional = state === 'connecting';

  const handleClick = () => {
    if (isTransitional) return;
    if (state === 'connected') {
      onDisconnect();
    } else {
      onConnect();
    }
  };

  const label = {
    stopped: t('connect'),
    connecting: t('connecting'),
    connected: t('connected'),
  }[state];

  return (
    <button
      onClick={handleClick}
      disabled={isTransitional}
      className={cn(buttonVariants({ vpnState: state }), className)}
      aria-label={label}
    >
      {label}
    </button>
  );
}
