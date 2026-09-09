export type WebConfig = { workspace: string; model: string; mode: string; policy: Record<string, number>; ready: boolean; read_only_reason?: string };
export type WebSession = { ID: string; Label: string; Task: string; Model: string; Workdir: string; Status: string; CreatedAt: string; ParentID?: string; Children?: WebSession[] };
export type SessionDetail = { session: WebSession; owner: "daemon" | "external" | "inactive"; can_resume: boolean; can_stop: boolean; can_steer: boolean; can_rename: boolean };

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, headers: { "Content-Type": "application/json", ...init?.headers } });
  if (!response.ok) throw new Error((await response.text()).trim() || `Request failed (${response.status})`);
  return response.json() as Promise<T>;
}
export const body = (value: unknown) => JSON.stringify(value);
export const sessionPath = (id: string) => `/api/sessions/${encodeURIComponent(id)}`;

export function flattenSessions(items: WebSession[]): WebSession[] {
  return items.flatMap(item => [item, ...flattenSessions(item.Children || [])]);
}
