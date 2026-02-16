import { useState, useRef, useEffect } from 'react';
import { useTranslation } from 'react-i18next';

const DEBUG_TAP_THRESHOLD = 5;
const DEBUG_TAP_TIMEOUT_MS = 2000;

export function Settings() {
  const { t, i18n } = useTranslation('settings');
  const [tapCount, setTapCount] = useState(0);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const changeLanguage = (lang: string) => {
    i18n.changeLanguage(lang);
  };

  const handleVersionTap = () => {
    const nextCount = tapCount + 1;

    if (nextCount >= DEBUG_TAP_THRESHOLD) {
      window.location.href = '/debug.html';
      return;
    }

    setTapCount(nextCount);

    // Clear previous timeout and set a new one to reset the counter
    if (timeoutRef.current !== null) {
      clearTimeout(timeoutRef.current);
    }
    timeoutRef.current = setTimeout(() => {
      setTapCount(0);
      timeoutRef.current = null;
    }, DEBUG_TAP_TIMEOUT_MS);
  };

  useEffect(() => {
    return () => {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, []);

  return (
    <div className="p-4 space-y-6">
      <h1 className="text-xl font-semibold">{t('title')}</h1>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <span>{t('language')}</span>
          <select
            value={i18n.language}
            onChange={(e) => changeLanguage(e.target.value)}
            className="border rounded px-2 py-1"
          >
            <option value="zh-CN">中文</option>
            <option value="en-US">English</option>
          </select>
        </div>

        <div className="flex items-center justify-between">
          <span>{t('version')}</span>
          <span
            className="text-gray-500 cursor-pointer select-none"
            onClick={handleVersionTap}
          >
            0.4.0
          </span>
        </div>

        <div className="pt-4">
          <h2 className="font-medium mb-2">{t('about')}</h2>
          <p className="text-gray-500 text-sm">{t('aboutText')}</p>
        </div>
      </div>
    </div>
  );
}
