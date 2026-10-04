// 扫描结束后前端会用 ListTerminals/ListChats 整表替换镜像。
// 若这次列表在用户按下回车之前就发出，回来时会把刚刚写入的标题和 Prompted 冲掉，
// 表现为「发送后列表和页签标题都不变，要等下一次扫描」。

export interface MirrorRow {
  ID: string;
  Prompted?: boolean;
  Title: string;
  Status: string;
  SessionID: string;
}

export function mergeMirrorList<T extends MirrorRow>(prev: T[], next: T[]): T[] {
  const byID = new Map(prev.map((row) => [row.ID, row]));
  return next.map((row) => {
    const old = byID.get(row.ID);
    if (!old?.Prompted || row.Prompted) return row;
    return {
      ...row,
      Prompted: true,
      Title: old.Title || row.Title,
      Status: old.Status === 'running' && row.Status === 'ready' ? old.Status : row.Status,
      SessionID: row.SessionID || old.SessionID,
    };
  });
}
