import React from 'react';

interface FlagIconProps {
  locale: string;
  className?: string;
}

const FlagIcon: React.FC<FlagIconProps> = ({ locale, className = "w-4 h-4" }) => {
  const getFlagComponent = (locale: string) => {
    switch (locale) {
      case 'zh-CN':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#DE2910"/>
            <polygon points="3,2 4.5,4.5 7,3.5 5.5,1.5" fill="#FFDE00"/>
            <polygon points="8,1 8.5,2.5 10,2 9.5,0.5" fill="#FFDE00"/>
            <polygon points="8,3 8.5,4.5 10,4 9.5,2.5" fill="#FFDE00"/>
            <polygon points="8,5 8.5,6.5 10,6 9.5,4.5" fill="#FFDE00"/>
            <polygon points="8,7 8.5,8.5 10,8 9.5,6.5" fill="#FFDE00"/>
          </svg>
        );
      case 'zh-TW':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#FE0000"/>
            <rect width="12" height="8" fill="#000095"/>
            <circle cx="6" cy="4" r="2.5" fill="white" stroke="#000095" strokeWidth="0.5"/>
            <polygon points="6,1.5 6.5,3 8,3 6.75,4 7.25,5.5 6,4.5 4.75,5.5 5.25,4 4,3 5.5,3" fill="#000095"/>
          </svg>
        );
      case 'zh-HK':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#DE2910"/>
            <g transform="translate(12,8)">
              <circle r="3" fill="white"/>
              <path d="M-1.5,-1.5 L1.5,1.5 M1.5,-1.5 L-1.5,1.5 M0,-2.5 L0,2.5 M-2.5,0 L2.5,0" 
                    stroke="#DE2910" strokeWidth="0.3"/>
              <g transform="scale(0.8)">
                <path d="M0,-2 Q-1,-1 -2,0 Q-1,1 0,2 Q1,1 2,0 Q1,-1 0,-2 Z" fill="white"/>
                <circle r="0.5" fill="#DE2910"/>
              </g>
            </g>
          </svg>
        );
      // en-US is the single English locale (en-GB / en-AU fold into it), so it
      // carries the US flag; the others are one country each. Flags that are
      // white at the edge (JP, KR, RU) get a hairline so they don't vanish on a
      // light menu.
      case 'en-US':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#B22234"/>
            {[1, 3, 5, 7, 9, 11].map((i) => (
              <rect key={i} y={(i * 16) / 13} width="24" height={16 / 13} fill="#FFFFFF"/>
            ))}
            <rect width="9.6" height={(7 * 16) / 13} fill="#3C3B6E"/>
            {[1.6, 4, 6.4, 8.8].flatMap((x, c) =>
              [1.4, 3.4, 5.4, 7.4].slice(0, c % 2 === 0 ? 4 : 3).map((y, r) => (
                <circle key={`${c}-${r}`} cx={x} cy={c % 2 === 0 ? y : y + 1} r="0.45" fill="#FFFFFF"/>
              ))
            )}
          </svg>
        );
      case 'ja':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#FFFFFF"/>
            <circle cx="12" cy="8" r="4.8" fill="#BC002D"/>
            <rect x="0.25" y="0.25" width="23.5" height="15.5" fill="none" stroke="#D1D5DB" strokeWidth="0.5"/>
          </svg>
        );
      case 'ru':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#FFFFFF"/>
            <rect y="5.333" width="24" height="5.334" fill="#0039A6"/>
            <rect y="10.667" width="24" height="5.333" fill="#D52B1E"/>
            <rect x="0.25" y="0.25" width="23.5" height="15.5" fill="none" stroke="#D1D5DB" strokeWidth="0.5"/>
          </svg>
        );
      case 'ko':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#FFFFFF"/>
            <path d="M8.4 8 A3.6 3.6 0 0 1 15.6 8 A1.8 1.8 0 0 1 12 8 A1.8 1.8 0 0 0 8.4 8 Z" fill="#CD2E3A"/>
            <path d="M15.6 8 A3.6 3.6 0 0 1 8.4 8 A1.8 1.8 0 0 1 12 8 A1.8 1.8 0 0 0 15.6 8 Z" fill="#0047A0"/>
            {/* trigrams: three bars in each corner, pointing at the centre */}
            {[
              { x: 4.6, y: 3.3, r: 33 },
              { x: 19.4, y: 3.3, r: -33 },
              { x: 4.6, y: 12.7, r: -33 },
              { x: 19.4, y: 12.7, r: 33 },
            ].map(({ x, y, r }) => (
              <g key={`${x}-${y}`} transform={`translate(${x} ${y}) rotate(${r})`} fill="#000000">
                <rect x="-2" y="-1.2" width="4" height="0.6"/>
                <rect x="-2" y="-0.3" width="4" height="0.6"/>
                <rect x="-2" y="0.6" width="4" height="0.6"/>
              </g>
            ))}
            <rect x="0.25" y="0.25" width="23.5" height="15.5" fill="none" stroke="#D1D5DB" strokeWidth="0.5"/>
          </svg>
        );
      // Any other locale: a language-code badge (a language is not a country).
      default:
        return (
          <div className={`${className} bg-muted rounded-sm flex items-center justify-center`}>
            <span className="text-[8px] font-semibold leading-none text-muted-foreground">
              {locale.slice(0, 2).toUpperCase()}
            </span>
          </div>
        );
    }
  };

  return getFlagComponent(locale);
};

export default FlagIcon;