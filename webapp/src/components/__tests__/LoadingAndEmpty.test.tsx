/**
 * Loading 和 Empty 状态组件测试
 *
 * 测试 Loading 和 Empty 状态组件的基本渲染
 */
import { describe, it, expect } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../../test/utils/render';
import { LoadingState, EmptyState, LoadingCard, EmptyCard } from '../LoadingAndEmpty';
import SentimentDissatisfiedIcon from '@mui/icons-material/SentimentDissatisfied';

describe('LoadingState', () => {
  it('应该渲染 loading 状态', () => {
    render(<LoadingState />);

    // 检查是否有 progressbar
    expect(screen.getByRole('progressbar')).toBeInTheDocument();
  });

  it('应该显示自定义消息', () => {
    render(<LoadingState message="正在加载数据..." />);

    expect(screen.getByText('正在加载数据...')).toBeInTheDocument();
  });
});

describe('EmptyState', () => {
  it('应该渲染标题', () => {
    render(<EmptyState title="没有数据" />);

    expect(screen.getByText('没有数据')).toBeInTheDocument();
  });

  it('应该渲染描述文本', () => {
    render(<EmptyState title="空" description="暂无可用数据" />);

    expect(screen.getByText('暂无可用数据')).toBeInTheDocument();
  });

  it('应该渲染图标', () => {
    render(<EmptyState title="空" icon={<SentimentDissatisfiedIcon data-testid="empty-icon" />} />);

    expect(screen.getByTestId('empty-icon')).toBeInTheDocument();
  });

  it('没有描述时不应该渲染描述元素', () => {
    render(<EmptyState title="空" />);

    expect(screen.queryByText('暂无可用数据')).not.toBeInTheDocument();
  });
});

describe('LoadingCard', () => {
  it('应该在卡片中渲染 loading 状态', () => {
    render(<LoadingCard />);

    expect(screen.getByRole('progressbar')).toBeInTheDocument();
  });

  it('应该显示自定义消息', () => {
    render(<LoadingCard message="加载中..." />);

    expect(screen.getByText('加载中...')).toBeInTheDocument();
  });
});

describe('EmptyCard', () => {
  it('应该在卡片中渲染子内容', () => {
    render(
      <EmptyCard>
        <div data-testid="child-content">子内容</div>
      </EmptyCard>
    );

    expect(screen.getByTestId('child-content')).toBeInTheDocument();
    expect(screen.getByText('子内容')).toBeInTheDocument();
  });
});
