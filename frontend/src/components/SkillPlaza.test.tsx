import '@testing-library/jest-dom/vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { tt } from '../test/i18n';
import { SkillPlaza } from './SkillPlaza';

vi.mock('../lib/api', () => ({
  searchSkills: vi.fn(async () => [
    { ID: 'a/b/c', SkillID: 'c', Name: 'demo-skill', Source: 'a/b', Installs: 12 },
  ]),
  listInstalledSkills: vi.fn(async () => []),
  listSkillTargets: vi.fn(async () => [
    { ToolID: 'claude', Root: 'C:\\Users\\me\\.claude\\skills', DefaultChecked: true },
  ]),
  getSkillDetail: vi.fn(async () => ({
    ID: 'a/b/c',
    Name: 'demo-skill',
    Description: 'desc',
    Source: 'a/b',
    BodyPreview: 'body',
  })),
  installSkill: vi.fn(async () => ({ ID: 'a/b/c', Name: 'demo-skill', Targets: { claude: { Mode: 'link', Path: 'x' } } })),
  uninstallSkill: vi.fn(async () => {}),
}));

vi.mock('../state/store', () => ({
  useAppStore: (sel: (s: { notify: () => void }) => unknown) =>
    sel({ notify: vi.fn() }),
}));

describe('SkillPlaza', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  afterEach(() => cleanup());

  it('renders search results from recommend query', async () => {
    render(<SkillPlaza />);
    await waitFor(() => {
      expect(screen.getByText('demo-skill')).toBeInTheDocument();
    });
    expect(screen.getByText(tt('ui.settings.skills.recommend'))).toBeInTheDocument();
    expect(screen.getByRole('button', { name: tt('ui.settings.skills.install') })).toBeInTheDocument();
  });
});
