// The console's API client.
//
// Every call returns something already composed for a screen — a queue, a case —
// rather than library resources the browser would have to assemble. That is
// deliberate: domain rules live in Go, where a shortcut here cannot bypass them.

export type Staff = {
  reference: string;
  name: string;
  title: string;
  groups: string[];
};

export type Session = {
  signedInAs: Staff | null;
  roster: Staff[];
  agentMode: string;
};

export type QueueItem = {
  reference: string;
  type: "claim" | "application";
  stepName: string;
  assignee: string;
  waitingSince: string;
  isDraft: boolean;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
  });

  if (!response.ok) {
    throw new Error((await response.text()) || response.statusText);
  }

  return (await response.json()) as T;
}

export const api = {
  session: () => request<Session>("/api/session"),
  signIn: (reference: string) =>
    request<Staff>("/api/session", {
      method: "POST",
      body: JSON.stringify({ reference }),
    }),
  queue: () => request<QueueItem[]>("/api/queue"),
};
