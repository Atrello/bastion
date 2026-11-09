// The panel's API, one function per endpoint. With VITE_DEMO set (npm run demo) the answers come from
// demo.ts instead, so the screens can be worked on without a panel or any servers.
import type { ComposeVersion, Container, Server, Stack, StackFiles } from './types';

const DEMO = import.meta.env.VITE_DEMO === '1';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  if (DEMO) {
    const { demo } = await import('./demo');
    return demo(method, path, body) as T;
  }
  const res = await fetch(`/api${path}`, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    const text = await res.text();
    let message = text || res.statusText;
    try {
      message = JSON.parse(text).error ?? message;
    } catch {
      // not JSON: use the text as it is
    }
    throw new ApiError(message, res.status);
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

const stackPath = (serverId: number, name: string) => `/servers/${serverId}/stacks/${encodeURIComponent(name)}`;

export type StackAction = 'up' | 'down' | 'stop' | 'start' | 'restart' | 'pull' | 'update';

export const api = {
  servers: () => request<Server[]>('GET', '/servers'),
  addServer: (name: string) => request<{ server: Server; joinCommand: string }>('POST', '/servers', { name }),
  stacks: () => request<Stack[]>('GET', '/stacks'),
  containers: () => request<Container[]>('GET', '/containers'),
  stackFiles: (serverId: number, name: string) => request<StackFiles>('GET', `${stackPath(serverId, name)}/files`),
  saveStack: (serverId: number, name: string, files: StackFiles) => request<void>('PUT', `${stackPath(serverId, name)}/files`, files),
  /** The last lines of a stack's logs, all its containers together. */
  stackLogs: (serverId: number, name: string, tail = 200) =>
    request<{ lines: string[] }>('GET', `${stackPath(serverId, name)}/logs?tail=${tail}`),
  stackHistory: (serverId: number, name: string) => request<ComposeVersion[]>('GET', `${stackPath(serverId, name)}/history`),
  stackAction: (serverId: number, name: string, action: StackAction) =>
    request<{ output: string }>('POST', `${stackPath(serverId, name)}/actions/${action}`),
};
