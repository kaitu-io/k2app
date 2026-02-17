/**
 * 自定义 render 函数
 *
 * 包装 React Testing Library 的 render，自动添加必要的 Provider
 */
import React, { ReactElement, ReactNode } from 'react';
import { render, RenderOptions, RenderResult } from '@testing-library/react';
import { BrowserRouter, MemoryRouter } from 'react-router-dom';
import { ThemeProvider, createTheme } from '@mui/material/styles';
import { I18nextProvider } from 'react-i18next';
import i18n from '../../i18n/i18n';

// 创建默认主题
const defaultTheme = createTheme({
  palette: {
    mode: 'light',
  },
});

// Provider 配置类型
interface CustomRenderOptions extends Omit<RenderOptions, 'wrapper'> {
  /** 初始路由路径 */
  initialRoute?: string;
  /** 路由条目（用于 MemoryRouter） */
  initialEntries?: string[];
  /** 是否使用 MemoryRouter（默认使用 BrowserRouter） */
  useMemoryRouter?: boolean;
  /** 自定义主题 */
  theme?: ReturnType<typeof createTheme>;
  /** 额外的 Provider */
  extraProviders?: React.ComponentType<{ children: ReactNode }>[];
}

/**
 * 创建所有 Provider 的包装组件
 */
function createWrapper(options: CustomRenderOptions = {}): React.FC<{ children: ReactNode }> {
  const {
    initialRoute = '/',
    initialEntries,
    useMemoryRouter = false,
    theme = defaultTheme,
    extraProviders = [],
  } = options;

  return function Wrapper({ children }: { children: ReactNode }) {
    // 选择路由类型
    const RouterComponent = useMemoryRouter ? MemoryRouter : BrowserRouter;
    const routerProps = useMemoryRouter
      ? { initialEntries: initialEntries || [initialRoute] }
      : {};

    // 构建 Provider 层级
    let content = (
      <I18nextProvider i18n={i18n}>
        <ThemeProvider theme={theme}>
          <RouterComponent {...routerProps}>{children}</RouterComponent>
        </ThemeProvider>
      </I18nextProvider>
    );

    // 添加额外的 Provider
    for (const Provider of extraProviders.reverse()) {
      content = <Provider>{content}</Provider>;
    }

    return content;
  };
}

/**
 * 自定义 render 函数
 *
 * @example
 * ```tsx
 * // 基本使用
 * const { getByText } = customRender(<MyComponent />);
 *
 * // 指定初始路由
 * customRender(<MyComponent />, { initialRoute: '/dashboard' });
 *
 * // 使用 MemoryRouter（用于测试导航）
 * customRender(<MyComponent />, {
 *   useMemoryRouter: true,
 *   initialEntries: ['/login', '/dashboard'],
 * });
 * ```
 */
export function customRender(
  ui: ReactElement,
  options: CustomRenderOptions = {}
): RenderResult {
  const { initialRoute, initialEntries, useMemoryRouter, theme, extraProviders, ...renderOptions } =
    options;

  return render(ui, {
    wrapper: createWrapper({
      initialRoute,
      initialEntries,
      useMemoryRouter,
      theme,
      extraProviders,
    }),
    ...renderOptions,
  });
}

/**
 * 创建暗色主题 render
 */
export function renderDark(
  ui: ReactElement,
  options: CustomRenderOptions = {}
): RenderResult {
  const darkTheme = createTheme({
    palette: {
      mode: 'dark',
    },
  });

  return customRender(ui, { ...options, theme: darkTheme });
}

// 重新导出 React Testing Library 的所有工具
export * from '@testing-library/react';

// 默认导出自定义 render
export { customRender as render };
