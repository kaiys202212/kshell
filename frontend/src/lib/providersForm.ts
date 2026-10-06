// 自定义工具表单辅助：会话 glob 与空 spec。

export type CustomProviderSpec = {
  ID: string;
  Name: string;
  Detect: { Command: string; Dirs: string[] };
  Sessions: { Glob: string; Format: string };
  Fields: {
    CWD: string;
    ID: string;
    Timestamp: string;
    Title: string;
    TitleFallbacks: string[];
  };
  Resume: { Args: string[] };
  Permission: { BypassArgs: string[] };
  Verified: boolean;
};

export function emptyCustomProvider(): CustomProviderSpec {
  return {
    ID: '',
    Name: '',
    Detect: { Command: '', Dirs: [] },
    Sessions: { Glob: '', Format: 'jsonl' },
    Fields: {
      CWD: 'cwd',
      ID: 'id',
      Timestamp: 'timestamp',
      Title: 'title',
      TitleFallbacks: [],
    },
    Resume: { Args: ['--resume', '{id}'] },
    Permission: { BypassArgs: [] },
    Verified: false,
  };
}

export function sessionGlobFromDir(dir: string): string {
  const n = dir.replace(/\\/g, '/').replace(/\/+$/, '');
  if (!n) return '';
  return `${n}/*/*.jsonl`;
}

export function withoutBuiltinSpecs(list: CustomProviderSpec[] | null | undefined): CustomProviderSpec[] {
  const skip = new Set(['claude', 'codex', 'cursor', 'codebuddy', 'gemini', 'opencode']);
  return (list ?? []).filter((s) => !s.ID || !skip.has(s.ID)).map((s) => normalizeSpec(s));
}

export function normalizeSpec(raw: Partial<CustomProviderSpec> | undefined | null): CustomProviderSpec {
  const e = emptyCustomProvider();
  if (!raw) return e;
  return {
    ID: raw.ID ?? e.ID,
    Name: raw.Name ?? e.Name,
    Detect: { Command: raw.Detect?.Command ?? '', Dirs: raw.Detect?.Dirs ?? [] },
    Sessions: {
      Glob: raw.Sessions?.Glob ?? '',
      Format: raw.Sessions?.Format || 'jsonl',
    },
    Fields: {
      CWD: raw.Fields?.CWD ?? e.Fields.CWD,
      ID: raw.Fields?.ID ?? e.Fields.ID,
      Timestamp: raw.Fields?.Timestamp ?? e.Fields.Timestamp,
      Title: raw.Fields?.Title ?? e.Fields.Title,
      TitleFallbacks: raw.Fields?.TitleFallbacks ?? [],
    },
    Resume: { Args: raw.Resume?.Args ?? e.Resume.Args },
    Permission: { BypassArgs: raw.Permission?.BypassArgs ?? [] },
    Verified: Boolean(raw.Verified),
  };
}
