/**
 * 自备教程第 3 步：命令块是占位凭证的示例，不能被当成可直接运行的命令复制走。
 */
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';

vi.mock('next-intl', () => {
  const t = Object.assign((key: string) => key, { raw: () => [] });
  return { useTranslations: () => t };
});

vi.mock('@/i18n/routing', () => ({
  Link: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
}));

import { Step3InstallK2r } from '../Step3InstallK2r';

describe('Step3InstallK2r', () => {
  it('命令块标「示例」、没有复制按钮，并引导去「我的路由器」取完整命令', () => {
    render(<Step3InstallK2r />);
    expect(screen.getByTestId('k2r-command-example-label')).toHaveTextContent('exampleLabel');
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.getByText(/edition\.diy\.commandPlaceholder/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'edition.diy.commandHint' })).toHaveAttribute('href', '/account/router');
  });
});
