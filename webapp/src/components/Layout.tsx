import { useState, useEffect, useRef } from 'react';
import { useLocation, useOutlet } from 'react-router-dom';
import { BottomNav } from './BottomNav';
import { BackButton } from './BackButton';

const TAB_PATHS = ['/', '/purchase', '/invite', '/account'];

export function Layout() {
  const location = useLocation();
  const outlet = useOutlet();
  const [visitedTabs, setVisitedTabs] = useState<Record<string, React.ReactNode>>({});
  const currentPath = TAB_PATHS.includes(location.pathname)
    ? location.pathname
    : location.pathname;
  const isTabRoute = TAB_PATHS.includes(currentPath);

  // Cache visited tab content. We use a ref to track the latest outlet
  // for the current path so React state updates don't cause stale captures.
  const outletRef = useRef(outlet);
  outletRef.current = outlet;

  useEffect(() => {
    if (isTabRoute && outletRef.current) {
      setVisitedTabs((prev) => ({ ...prev, [currentPath]: outletRef.current }));
    }
  }, [currentPath, isTabRoute]);

  // Build the tabs to render: all visited tabs + current tab with fresh outlet
  const tabsToRender = isTabRoute
    ? { ...visitedTabs, [currentPath]: outlet }
    : visitedTabs;

  return (
    <div
      className="flex flex-col h-screen bg-bg-default"
      style={{ paddingTop: 'env(safe-area-inset-top, 0px)' }}
    >
      <main className="flex-1 overflow-y-auto relative">
        {isTabRoute ? (
          Object.entries(tabsToRender).map(([path, content]) => (
            <div
              key={path}
              style={{
                visibility: path === currentPath ? 'visible' : 'hidden',
                position: path === currentPath ? 'relative' : 'absolute',
                width: '100%',
                top: 0,
                left: 0,
              }}
            >
              {content}
            </div>
          ))
        ) : (
          <>
            <div className="px-2 pt-1">
              <BackButton />
            </div>
            {outlet}
          </>
        )}
      </main>
      {isTabRoute && <BottomNav />}
    </div>
  );
}
