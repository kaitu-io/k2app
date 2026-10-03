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
      default:
        return (
          <div className={`${className} bg-muted rounded-sm flex items-center justify-center`}>
            <span className="text-xs text-muted-foreground">{"?"}</span>
          </div>
        );
    }
  };

  return getFlagComponent(locale);
};

export default FlagIcon;