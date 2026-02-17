/**
 * DOM API 补丁 - 需要在 testing-library 加载前执行
 *
 * 这个文件修复 jsdom 中不完整的 DOM API 实现
 * 主要解决 @testing-library/dom 的 isInaccessible 函数
 * 调用 getComputedStyle 时返回 undefined 的问题
 */

// 创建一个完整的 mock CSSStyleDeclaration
const createMockCSSStyleDeclaration = (): CSSStyleDeclaration => {
  const styles: Record<string, string> = {
    visibility: 'visible',
    display: 'block',
    opacity: '1',
    width: '100px',
    height: '100px',
    minHeight: '0px',
    maxHeight: 'none',
    minWidth: '0px',
    maxWidth: 'none',
    position: 'static',
    top: 'auto',
    right: 'auto',
    bottom: 'auto',
    left: 'auto',
    margin: '0px',
    padding: '0px',
    border: '0px',
    outline: 'none',
    transform: 'none',
    transition: 'none',
    animation: 'none',
    overflow: 'visible',
    zIndex: 'auto',
    pointerEvents: 'auto',
    backgroundColor: 'transparent',
    color: 'rgb(0, 0, 0)',
    fontSize: '16px',
    fontWeight: 'normal',
    lineHeight: 'normal',
    textAlign: 'left',
    textDecoration: 'none',
    cursor: 'auto',
    boxSizing: 'content-box',
    flexDirection: 'row',
    justifyContent: 'flex-start',
    alignItems: 'stretch',
  };

  // 使用 Proxy 来处理任意属性访问
  const handler: ProxyHandler<Record<string, string>> = {
    get(target, prop) {
      // 特殊方法处理
      if (prop === 'getPropertyValue') {
        return (property: string): string => {
          const camelCase = property.replace(/-([a-z])/g, (g) => g[1].toUpperCase());
          return target[camelCase] || target[property] || '';
        };
      }
      if (prop === 'getPropertyPriority') {
        return (): string => '';
      }
      if (prop === 'item') {
        return (index: number): string => Object.keys(target)[index] || '';
      }
      if (prop === 'setProperty') {
        return (property: string, value: string | null): void => {
          if (value !== null) {
            target[property] = value;
          }
        };
      }
      if (prop === 'removeProperty') {
        return (property: string): string => {
          const value = target[property];
          delete target[property];
          return value || '';
        };
      }
      if (prop === 'length') {
        return Object.keys(target).length;
      }
      if (prop === 'parentRule') {
        return null;
      }
      if (prop === 'cssFloat') {
        return '';
      }
      if (prop === 'cssText') {
        return '';
      }
      if (prop === Symbol.iterator) {
        return function* () {
          for (const key of Object.keys(target)) {
            yield key;
          }
        };
      }

      // 属性访问 - 返回值或空字符串（不返回 undefined）
      if (typeof prop === 'string') {
        return target[prop] !== undefined ? target[prop] : '';
      }
      return undefined;
    },
  };

  return new Proxy(styles, handler) as unknown as CSSStyleDeclaration;
};

// 完全替换 window.getComputedStyle
// 不调用原始实现，因为 jsdom 的实现会返回 undefined
window.getComputedStyle = function (
  _element: Element,
  _pseudoElt?: string | null
): CSSStyleDeclaration {
  return createMockCSSStyleDeclaration();
};

// 导出以便其他地方使用
export { createMockCSSStyleDeclaration };
