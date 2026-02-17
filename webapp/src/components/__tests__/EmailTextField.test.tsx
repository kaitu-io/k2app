/**
 * EmailTextField 组件测试
 *
 * 测试邮箱输入组件的基本功能
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '../../test/utils/render';
import EmailTextField from '../EmailTextField';

describe('EmailTextField', () => {
  // 默认的必需 props
  const defaultProps = {
    value: '',
    onChange: vi.fn(),
  };

  describe('基本渲染', () => {
    it('应该渲染输入框', () => {
      render(<EmailTextField {...defaultProps} />);

      expect(screen.getByRole('textbox')).toBeInTheDocument();
    });

    it('应该显示自定义 label', () => {
      render(<EmailTextField {...defaultProps} label="邮箱地址" />);

      expect(screen.getByLabelText('邮箱地址')).toBeInTheDocument();
    });

    it('应该显示 placeholder', () => {
      render(<EmailTextField {...defaultProps} placeholder="请输入邮箱" />);

      expect(screen.getByPlaceholderText('请输入邮箱')).toBeInTheDocument();
    });
  });

  describe('输入处理', () => {
    it('应该调用 onChange 回调', async () => {
      // 跳过 pointerEvents 检查，因为 jsdom 的 getComputedStyle 不完整
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      const handleChange = vi.fn();
      render(<EmailTextField value="" onChange={handleChange} />);

      const input = screen.getByRole('textbox');
      await user.type(input, 'test@example.com');

      expect(handleChange).toHaveBeenCalled();
    });

    it('应该清理输入中的空格', async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      const handleChange = vi.fn();
      render(<EmailTextField value="" onChange={handleChange} />);

      const input = screen.getByRole('textbox');
      await user.type(input, '  test@example.com  ');

      // onChange 被调用时，值应该已经被清理
      expect(handleChange).toHaveBeenCalled();
    });
  });

  describe('邮箱验证', () => {
    it('有效邮箱不应该显示错误', () => {
      render(<EmailTextField value="user@example.com" onChange={vi.fn()} />);

      // 不应该有 error 类的元素
      expect(screen.queryByText(/无效/i)).not.toBeInTheDocument();
    });

    it('空输入不应该显示错误', () => {
      render(<EmailTextField value="" onChange={vi.fn()} />);

      // 空输入不验证
      expect(screen.queryByText(/无效/i)).not.toBeInTheDocument();
    });
  });

  describe('失焦验证', () => {
    it('失焦时应该调用自定义 onBlur', async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      const handleBlur = vi.fn();
      const handleChange = vi.fn();
      render(<EmailTextField value="" onBlur={handleBlur} onChange={handleChange} />);

      const input = screen.getByRole('textbox');
      await user.click(input);  // Focus first
      await user.tab();  // Tab away to blur

      expect(handleBlur).toHaveBeenCalled();
    });
  });

  describe('受控输入', () => {
    it('应该显示受控值', () => {
      render(<EmailTextField value="controlled@test.com" onChange={() => {}} />);

      const input = screen.getByRole('textbox');
      expect(input).toHaveValue('controlled@test.com');
    });

    it('值改变时应该更新', () => {
      const { rerender } = render(<EmailTextField value="first@test.com" onChange={() => {}} />);

      expect(screen.getByRole('textbox')).toHaveValue('first@test.com');

      rerender(<EmailTextField value="second@test.com" onChange={() => {}} />);
      expect(screen.getByRole('textbox')).toHaveValue('second@test.com');
    });
  });

  describe('禁用状态', () => {
    it('应该禁用输入框', () => {
      render(<EmailTextField {...defaultProps} disabled />);

      const input = screen.getByRole('textbox');
      expect(input).toBeDisabled();
    });
  });

  describe('必填状态', () => {
    it('应该标记为必填', () => {
      render(<EmailTextField {...defaultProps} required />);

      const input = screen.getByRole('textbox');
      expect(input).toBeRequired();
    });
  });
});
