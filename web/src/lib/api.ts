// The panel's API, one function per endpoint. With VITE_DEMO set (npm run demo) the answers come from
// demo.ts instead, so the screens can be worked on without a panel or any servers.
import type {
  Cleanup,
  ComposeVersion,
  Container,
  Egg,
  GitStack,
  Image,
  NpmInstance,
  NpmItem,
  NpmKind,
  NpmSummary,
  Volume,
  GameBackup,
  GameFile,
  GameServer,
  Schedule,
  Server,
  Stack,
  StackFiles,
  StartupVariable,
} from './types';

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
  if (res.status === 401 && !path.startsWith('/auth/') && path !== '/setup') {
    // The session has ended: the app goes back to the sign-in page.
    dispatchEvent(new Event('bastion:signed-out'));
  }
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

export type AuthState = { setupNeeded: boolean; signedIn: boolean; username?: string; passkeys: boolean };
export type Account = {
  username: string;
  hasAuthenticator: boolean;
  recoveryCodesLeft: number;
  passkeys: { id: number; name: string; createdAt: string; lastUsedAt: string | null }[];
};
type PasskeyOptions = { publicKey: Record<string, any> };

export type AlertsPage = {
  channels: { id: number; kind: 'ntfy' | 'discord' | 'email'; name: string; detail: string }[];
  kinds: string[];
  recent: { id: number; kind: string; title: string; detail: string; raisedAt: string; clearedAt: string | null }[];
};

const stackPath = (serverId: number, name: string) => `/servers/${serverId}/stacks/${encodeURIComponent(name)}`;

export type StackAction = 'up' | 'down' | 'stop' | 'start' | 'restart' | 'pull' | 'update';
export type GamePower = 'start' | 'stop' | 'restart' | 'kill';

export const api = {
  authState: () => request<AuthState>('GET', '/auth/state'),
  setup: (token: string, username: string, password: string) => request<void>('POST', '/setup', { token, username, password }),
  signIn: (username: string, password: string) => request<{ code: boolean; passkey: boolean }>('POST', '/auth/signin', { username, password }),
  signInWithCode: (code: string) => request<void>('POST', '/auth/signin/code', { code }),
  signInWithRecoveryCode: (code: string) => request<void>('POST', '/auth/signin/recovery', { code }),
  passkeyBegin: () => request<PasskeyOptions>('POST', '/auth/passkey/begin'),
  passkeyFinish: (answer: unknown) => request<void>('POST', '/auth/passkey/finish', answer),
  authenticatorBegin: (password = '') => request<{ secret: string; qrCode: string }>('POST', '/auth/authenticator/begin', { password }),
  authenticatorFinish: (code: string) => request<{ recoveryCodes: string[] }>('POST', '/auth/authenticator/finish', { code }),
  passkeyRegisterBegin: (name: string, password = '') => request<PasskeyOptions>('POST', '/auth/passkey/register/begin', { name, password }),
  passkeyRegisterFinish: (answer: unknown) => request<void>('POST', '/auth/passkey/register/finish', answer),
  signOut: () => request<void>('POST', '/auth/signout'),
  account: () => request<Account>('GET', '/account'),
  changePassword: (current: string, next: string) => request<void>('POST', '/account/password', { current, next }),
  newRecoveryCodes: (password: string) => request<{ recoveryCodes: string[] }>('POST', '/account/recovery-codes', { password }),
  deletePasskey: (id: number) => request<void>('DELETE', `/account/passkeys/${id}`),
  signOutEverywhere: () => request<void>('POST', '/account/signout-everywhere'),

  servers: () => request<Server[]>('GET', '/servers'),
  addServer: (name: string) => request<{ server: Server; joinCommand: string }>('POST', '/servers', { name }),
  stacks: () => request<Stack[]>('GET', '/stacks'),
  containers: () => request<Container[]>('GET', '/containers'),
  /** Asks every server to check its images' registries now; results arrive a few seconds later. */
  checkUpdates: () => request<void>('POST', '/updates/check'),
  volumes: (serverId: number) => request<Volume[]>('GET', `/servers/${serverId}/volumes`),
  images: (serverId: number) => request<Image[]>('GET', `/servers/${serverId}/images`),
  cleanupPreview: (serverId: number) => request<Cleanup>('GET', `/servers/${serverId}/cleanup`),
  cleanup: (serverId: number, what: { images: boolean; volumes: boolean; buildCache: boolean }) =>
    request<{ output: string }>('POST', `/servers/${serverId}/cleanup`, what),
  stackFiles: (serverId: number, name: string) => request<StackFiles>('GET', `${stackPath(serverId, name)}/files`),
  saveStack: (serverId: number, name: string, files: StackFiles) => request<void>('PUT', `${stackPath(serverId, name)}/files`, files),
  /** The last lines of a stack's logs, all its containers together. */
  stackLogs: (serverId: number, name: string, tail = 200) =>
    request<{ lines: string[] }>('GET', `${stackPath(serverId, name)}/logs?tail=${tail}`),
  createStack: (serverId: number, name: string, compose: string, env: string | null) =>
    request<{ output: string }>('POST', `/servers/${serverId}/stacks`, { name, compose, env }),
  git: (serverId: number, name: string) => request<GitStack | null>('GET', `${stackPath(serverId, name)}/git`),
  setGit: (serverId: number, name: string, git: { repo: string; branch: string; path: string; autoDeploy: boolean }) =>
    request<GitStack>('PUT', `${stackPath(serverId, name)}/git`, git),
  unlinkGit: (serverId: number, name: string) => request<void>('DELETE', `${stackPath(serverId, name)}/git`),
  deployGit: (serverId: number, name: string) => request<{ output: string }>('POST', `${stackPath(serverId, name)}/git/deploy`),
  stackHistory: (serverId: number, name: string) => request<ComposeVersion[]>('GET', `${stackPath(serverId, name)}/history`),
  stackAction: (serverId: number, name: string, action: StackAction) =>
    request<{ output: string }>('POST', `${stackPath(serverId, name)}/actions/${action}`),

  games: () => request<GameServer[]>('GET', '/games'),
  game: (id: number) => request<GameServer>('GET', `/games/${id}`),
  createGame: (game: { name: string; eggId: number; serverId: number; memoryMb: number; port: number }) =>
    request<GameServer>('POST', '/games', game),
  gamePower: (id: number, action: GamePower) => request<void>('POST', `/games/${id}/power/${action}`),
  /** The console's recent output; new lines then arrive over the console's websocket. */
  gameConsole: (id: number) => request<{ lines: string[] }>('GET', `/games/${id}/console`),
  gameCommand: (id: number, command: string) => request<void>('POST', `/games/${id}/command`, { command }),
  gameFiles: (id: number, path: string) => request<GameFile[]>('GET', `/games/${id}/files?path=${encodeURIComponent(path)}`),
  gameStartup: (id: number) => request<{ command: string; image: string; variables: StartupVariable[] }>('GET', `/games/${id}/startup`),
  saveGameStartup: (id: number, values: Record<string, string>) => request<void>('PUT', `/games/${id}/startup`, values),
  gameSchedules: (id: number) => request<Schedule[]>('GET', `/games/${id}/schedules`),
  gameBackups: (id: number) => request<GameBackup[]>('GET', `/games/${id}/backups`),
  createGameBackup: (id: number) => request<GameBackup>('POST', `/games/${id}/backups`),
  alerts: () => request<AlertsPage>('GET', '/alerts'),
  setAlertKinds: (kinds: string[]) => request<void>('PUT', '/alerts/kinds', { kinds }),
  addChannel: (kind: string, name: string, config: Record<string, string | number>) => request<void>('POST', '/alerts/channels', { kind, name, config }),
  deleteChannel: (id: number) => request<void>('DELETE', `/alerts/channels/${id}`),
  testChannel: (id: number) => request<void>('POST', `/alerts/channels/${id}/test`),
  npmInstances: () => request<NpmInstance[]>('GET', '/npm'),
  /** Links an NPM, signing in to it first to check the details. */
  linkNpm: (npm: { name: string; url: string; email: string; password: string }) => request<void>('POST', '/npm', npm),
  unlinkNpm: (id: number) => request<void>('DELETE', `/npm/${id}`),
  npmSummary: () => request<NpmSummary>('GET', '/npm/summary'),
  npmList: (id: number, kind: NpmKind) => request<NpmItem[]>('GET', `/npm/${id}/${kind}`),
  /** Creates an item (no itemId) or replaces one, sending it as NPM's API takes it. */
  npmSave: (id: number, kind: NpmKind, item: Record<string, unknown>, itemId?: number) =>
    itemId ? request<NpmItem>('PUT', `/npm/${id}/${kind}/${itemId}`, item) : request<NpmItem>('POST', `/npm/${id}/${kind}`, item),
  npmDelete: (id: number, kind: NpmKind, itemId: number) => request<void>('DELETE', `/npm/${id}/${kind}/${itemId}`),
  npmAction: (id: number, kind: NpmKind, itemId: number, action: 'enable' | 'disable' | 'renew') =>
    request<unknown>('POST', `/npm/${id}/${kind}/${itemId}/${action}`),
  /** Gives a custom certificate its files, as PEM text. */
  npmUpload: (id: number, certId: number, files: { certificate: string; certificateKey: string; intermediate: string }) =>
    request<void>('POST', `/npm/${id}/certificates/${certId}/upload`, files),
  eggs: () => request<Egg[]>('GET', '/eggs'),
  /** Imports a Pterodactyl egg: the egg's JSON file, as it's exported from Pterodactyl or a community repo. */
  importEgg: (json: string) => request<Egg>('POST', '/eggs', JSON.parse(json)),
};
