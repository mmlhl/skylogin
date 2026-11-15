import type { Account } from "./contract";

export type AccountListView = "flat" | "tag" | "channel" | "time";
export type AccountSort = "custom" | "name" | "recent" | "running";
export interface AccountSection {
  key: string;
  label: string;
  tag?: string;
  untagged?: boolean;
  rows: Account[];
  draggable: boolean;
}

export const tagIdentity = (tag: string) => tag.trim().toLowerCase();

/** 标签身份和显示文字分开，用户可自由使用“未分组”及带引号的标签。 */
export function orderedAccountTags(accounts: Account[], preferred: string[] = []): string[] {
  const names = new Map<string, string>();
  for (const a of accounts) for (const tag of a.tags) {
    const key = tagIdentity(tag);
    if (key && !names.has(key)) names.set(key, tag);
  }
  const keys = [...new Set([...preferred.map(tagIdentity), ...names.keys()])];
  return keys.filter((key) => names.has(key)).map((key) => names.get(key)!);
}

/** 段内排序只替换该段所占位置；其它账号不移动，多标签投影共享同一账号顺序。 */
export function replaceAccountSubsequence(global: string[], proposed: string[]): string[] {
  const existing = new Set(global);
  const members = [...new Set(proposed)].filter((id) => existing.has(id));
  const picked = new Set(members);
  let at = 0;
  return global.map((id) => picked.has(id) ? members[at++]! : id);
}

export function matchesAccount(a: Account, query: string, channel: (value: Account["channel"]) => string): boolean {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const text = [a.name, a.fullId, a.maskedName, a.maskedId, channel(a.channel), ...a.tags].join(" ").toLowerCase();
  return words.every((word) => text.includes(word));
}

export function accountTimeBucket(a: Account, now = Date.now()): string {
  if (!a.createdAt) return "30 天以上";
  const days = (now / 1000 - a.createdAt) / 86400;
  return days <= 7 ? "最近 7 天" : days <= 30 ? "8–30 天" : "30 天以上";
}
