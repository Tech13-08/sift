export type SetupStatus = {
  google?: boolean;
  discord: boolean;
  gmail: boolean;
  schedule: boolean;
  contact_email?: boolean;
};

export type Me = {
  id: string;
  username: string;
  email?: string | null;
  contact_email?: string | null;
  discord_id?: string | null;
  timezone: string;
  digest_local_time: string;
  setup: SetupStatus;
  mailboxes_need_relink: boolean;
  discord_server_invite?: string;
  discord_bot_invite?: string;
};

export type Mailbox = {
  email: string;
  status: "ok" | "relink" | "unknown";
  is_contact?: boolean;
};

export type Rule = {
  id: string;
  rule_type: string;
  pattern: string;
  instruction: string | null;
  color: number | null;
  created_at: string;
};

export type DigestListItem = {
  id: string;
  window_start: string;
  window_end: string;
  kind: string;
  status: string;
  summary: string | null;
  created_at: string;
};

export type DigestMessage = {
  id: string;
  mailbox: string;
  from_address: string;
  subject: string;
  fact_who: string | null;
  fact_what: string | null;
  fact_when: string | null;
  fact_summary: string | null;
  kind: string;
  outcome: string | null;
};

async function parseJSON<T>(res: Response): Promise<T> {
  const text = await res.text();
  if (!text) {
    return {} as T;
  }
  return JSON.parse(text) as T;
}

export async function apiGet<T>(path: string): Promise<{ ok: true; data: T } | { ok: false; status: number }> {
  const res = await fetch(path, { credentials: "include", cache: "no-store" });
  if (!res.ok) {
    return { ok: false, status: res.status };
  }
  return { ok: true, data: await parseJSON<T>(res) };
}

export async function apiSend<T>(
  path: string,
  method: string,
  body?: unknown
): Promise<
  | { ok: true; data: T; status: number }
  | { ok: false; status: number; error?: string; message?: string }
> {
  const res = await fetch(path, {
    method,
    credentials: "include",
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) {
    return { ok: true, data: {} as T, status: 204 };
  }
  const data = await parseJSON<T & { error?: string; message?: string }>(res);
  if (!res.ok) {
    return { ok: false, status: res.status, error: data?.error, message: data?.message };
  }
  return { ok: true, data, status: res.status };
}

export const RULE_COLORS: { name: string; value: number }[] = [
  { name: "grey", value: 0x95a5a6 },
  { name: "blue", value: 0x3498db },
  { name: "green", value: 0x2ecc71 },
  { name: "purple", value: 0x9b59b6 },
  { name: "red", value: 0xe74c3c },
  { name: "orange", value: 0xe67e22 },
  { name: "yellow", value: 0xf1c40f },
  { name: "pink", value: 0xe91e63 },
  { name: "teal", value: 0x1abc9c },
];

export const COMMON_TIMEZONES = [
  "America/Los_Angeles",
  "America/Denver",
  "America/Chicago",
  "America/New_York",
  "America/Anchorage",
  "Pacific/Honolulu",
  "UTC",
  "Europe/London",
  "Europe/Paris",
  "Europe/Berlin",
  "Asia/Tokyo",
  "Asia/Kolkata",
  "Asia/Singapore",
  "Australia/Sydney",
];
