import React from "react";
import * as FlagIcons from "country-flag-icons/react/3x2";
import { getCountryName } from "../i18n/countries";

export { getCountryName };

export function getFlagIcon(alpha2: string): JSX.Element | null {
  const key = alpha2?.toUpperCase() as keyof typeof FlagIcons;
  const Flag = FlagIcons[key];
  if (!Flag) return null;
  return React.createElement(Flag, { style: { width: 32, height: 22, borderRadius: 2, verticalAlign: 'middle' } });
} 