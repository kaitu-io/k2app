'use client';

import { createContext, useContext, type ReactNode } from 'react';
import { KAITU, type Brand } from '@/lib/brands';

const BrandContext = createContext<Brand>(KAITU);

export function BrandProvider({ brand, children }: { brand: Brand; children: ReactNode }) {
  return <BrandContext.Provider value={brand}>{children}</BrandContext.Provider>;
}

export function useBrand(): Brand {
  return useContext(BrandContext);
}
