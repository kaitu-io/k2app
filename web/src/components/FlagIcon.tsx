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
      case 'en-US':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#B22234"/>
            <rect width="24" height="1.23" y="1.23" fill="white"/>
            <rect width="24" height="1.23" y="3.69" fill="white"/>
            <rect width="24" height="1.23" y="6.15" fill="white"/>
            <rect width="24" height="1.23" y="8.62" fill="white"/>
            <rect width="24" height="1.23" y="11.08" fill="white"/>
            <rect width="24" height="1.23" y="13.54" fill="white"/>
            <rect width="9.6" height="8.62" fill="#3C3B6E"/>
          </svg>
        );
      case 'en-GB':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#012169"/>
            <path d="M0,0 L24,16 M24,0 L0,16" stroke="white" strokeWidth="1.6"/>
            <path d="M0,0 L24,16 M24,0 L0,16" stroke="#C8102E" strokeWidth="1"/>
            <path d="M12,0 L12,16 M0,8 L24,8" stroke="white" strokeWidth="2.4"/>
            <path d="M12,0 L12,16 M0,8 L24,8" stroke="#C8102E" strokeWidth="1.6"/>
          </svg>
        );
      case 'en-AU':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="#012169"/>
            {/* Union Jack in top left quarter */}
            <g transform="scale(0.5)">
              <path d="M0,0 L24,16 M24,0 L0,16" stroke="white" strokeWidth="1.6"/>
              <path d="M0,0 L24,16 M24,0 L0,16" stroke="#C8102E" strokeWidth="1"/>
              <path d="M12,0 L12,16 M0,8 L24,8" stroke="white" strokeWidth="2.4"/>
              <path d="M12,0 L12,16 M0,8 L24,8" stroke="#C8102E" strokeWidth="1.6"/>
            </g>
            {/* Southern Cross constellation */}
            <g fill="white">
              <circle cx="15" cy="6" r="0.8"/>
              <circle cx="17" cy="8" r="0.6"/>
              <circle cx="19" cy="10" r="0.8"/>
              <circle cx="16" cy="11" r="0.6"/>
              <circle cx="18" cy="13" r="0.7"/>
            </g>
            {/* Commonwealth Star */}
            <g transform="translate(6,12)">
              <polygon points="0,-1.5 0.5,-0.5 1.5,-0.5 0.75,0.25 1,1.5 0,0.75 -1,1.5 -0.75,0.25 -1.5,-0.5 -0.5,-0.5" fill="white"/>
            </g>
          </svg>
        );
      case 'ja':
        return (
          <svg className={className} viewBox="0 0 24 16" xmlns="http://www.w3.org/2000/svg">
            <rect width="24" height="16" fill="white"/>
            <circle cx="12" cy="8" r="4.8" fill="#BC002D"/>
          </svg>
        );
      default:
        return (
          <div className={`${className} bg-gray-300 rounded-sm flex items-center justify-center`}>
            <span className="text-xs text-gray-600">{"?"}</span>
          </div>
        );
    }
  };

  return getFlagComponent(locale);
};

export default FlagIcon;