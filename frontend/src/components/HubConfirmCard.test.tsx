import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import HubConfirmCard from './HubConfirmCard';
import { tt } from '../test/i18n';

afterEach(cleanup);

// makeMsg 构造一条 tool_confirm 形态的 Hub 消息（Text = Summary + 尾部 JSON）。
const makeMsg = (text: string) => ({
  id: 1,
  kind: 'agent',
  chatId: 'c1',
  event: 'tool_confirm',
  text,
  ts: 0,
});

describe('HubConfirmCard', () => {
  it('展示摘要（剥离尾部 JSON），确认与拒绝分别回调', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined);
    const text = '新建需求（项目 D:\\ws-a）：新增需求\n{"pending":"abc","tool":"create_requirement"}';
    render(<HubConfirmCard message={makeMsg(text)} onConfirm={onConfirm} />);

    expect(screen.getByText('新建需求（项目 D:\\ws-a）：新增需求')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.confirm_approve') }));
    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith('abc', true));
  });

  it('拒绝回调 approved=false', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined);
    const text = '删除需求 #r1\n{"pending":"p1","tool":"delete_requirement"}';
    render(<HubConfirmCard message={makeMsg(text)} onConfirm={onConfirm} />);

    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.confirm_reject') }));
    await waitFor(() => expect(onConfirm).toHaveBeenCalledWith('p1', false));
  });

  it('确认后进入已处理态，按钮消失', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined);
    const text = '新建需求\n{"pending":"p1","tool":"create_requirement"}';
    render(<HubConfirmCard message={makeMsg(text)} onConfirm={onConfirm} />);

    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.confirm_approve') }));
    await waitFor(() =>
      expect(screen.getByText(tt('ui.hub.confirm_done'))).toBeInTheDocument(),
    );
    expect(screen.queryByRole('button', { name: tt('ui.hub.confirm_approve') })).toBeNull();
  });

  it('确认失败展示错误并恢复按钮', async () => {
    const onConfirm = vi.fn().mockRejectedValue(new Error('boom'));
    const text = '新建需求\n{"pending":"p1","tool":"create_requirement"}';
    render(<HubConfirmCard message={makeMsg(text)} onConfirm={onConfirm} />);

    fireEvent.click(screen.getByRole('button', { name: tt('ui.hub.confirm_approve') }));
    await waitFor(() => expect(screen.getByText(/boom/)).toBeInTheDocument());
    expect(screen.getByRole('button', { name: tt('ui.hub.confirm_approve') })).toBeInTheDocument();
  });

  it('无 pending JSON 的消息不渲染操作按钮', () => {
    render(<HubConfirmCard message={makeMsg('一段没有尾部 JSON 的文本')} onConfirm={vi.fn()} />);
    expect(screen.queryByRole('button', { name: tt('ui.hub.confirm_approve') })).toBeNull();
  });
});
