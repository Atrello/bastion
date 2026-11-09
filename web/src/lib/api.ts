// The panel's API, one function per endpoint. With VITE_DEMO set (npm run demo) the answers come from
// demo.ts instead, so the screens can be worked on without a panel or any servers.
import type {
  Cleanup,
  ComposeVersion,
  Container,
  Egg,
  EggDetail,
  EggImage,
  GameShare,
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
  BackupStore,
  NotificationRule,
  Job,
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
    /** The whole answer, for the few that carry more than a message. */
    readonly body: Record<string, any> = {},
  ) {
    super(message);
  }
}

// The panel's build when this page loaded. A different one later means the panel was updated while the
// page was open: App.svelte offers to reload, as this page may not understand the new panel.
let loadedBuild: string | null = null;

function noticeBuild(res: Response) {
  const build = res.headers.get('X-Bastion-Build');
  if (!build) return;
  if (loadedBuild === null) loadedBuild = build;
  else if (build !== loadedBuild) dispatchEvent(new Event('bastion:updated'));
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
  noticeBuild(res);
  if (res.status === 401 && !path.startsWith('/auth/') && path !== '/setup') {
    // The session has ended: the app goes back to the sign-in page.
    dispatchEvent(new Event('bastion:signed-out'));
  }
  if (!res.ok) {
    const text = await res.text();
    let message = text || res.statusText;
    let body = {};
    try {
      body = JSON.parse(text);
      message = (body as { error?: string }).error ?? message;
    } catch {
      // not JSON: use the text as it is
    }
    throw new ApiError(message, res.status, body);
  }
  return res.status === 204 ? (undefined as T) : res.json();
}

export type AuthState = { setupNeeded: boolean; signedIn: boolean; username?: string; role?: 'owner' | 'friend'; passkeys: boolean; version?: string };
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
export type GameSettings = { name: string; image?: string; memoryMb: number; cpuPercent: number; diskMb: number; ports: number[]; warnCommand?: string };

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
  /** A fresh one-time command to join (or re-join) a server, for an hour. */
  newJoinCommand: (id: number) => request<{ joinCommand: string }>('POST', `/servers/${id}/join-command`),
  /** Forgets a server and disconnects its agent; what it runs keeps running. */
  /** Has the server's agent update itself to the panel's, then restart: back in a few seconds. */
  updateAgent: (id: number) => request<void>('POST', `/servers/${id}/agent/update`),
  deleteServer: (id: number) => request<void>('DELETE', `/servers/${id}`),
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
  createGame: (game: GameSettings & { eggId: number; serverId: number }) => request<GameServer>('POST', '/games', game),
  updateGame: (id: number, settings: GameSettings) => request<void>('PUT', `/games/${id}`, settings),
  deleteGame: (id: number) => request<void>('DELETE', `/games/${id}`),
  reinstallGame: (id: number) => request<void>('POST', `/games/${id}/reinstall`),
  /** Stop and restart can warn players first, then wait this many seconds. */
  gamePower: (id: number, action: GamePower, warn = 0) => request<void>('POST', `/games/${id}/power/${action}`, warn ? { warn } : undefined),
  /** The console's recent output; new lines then arrive over the game's stream. */
  gameConsole: (id: number) => request<{ lines: string[] }>('GET', `/games/${id}/console`),
  gameCommand: (id: number, command: string) => request<void>('POST', `/games/${id}/command`, { command }),
  gameFiles: (id: number, path: string) => request<GameFile[]>('GET', `/games/${id}/files?path=${encodeURIComponent(path)}`),
  readGameFile: (id: number, path: string) => request<{ content: string }>('GET', `/games/${id}/files/content?path=${encodeURIComponent(path)}`),
  writeGameFile: (id: number, path: string, content: string) => request<void>('PUT', `/games/${id}/files/content`, { path, content }),
  makeGameFolder: (id: number, path: string) => request<void>('POST', `/games/${id}/files/folder`, { path }),
  renameGameFile: (id: number, from: string, to: string) => request<void>('POST', `/games/${id}/files/rename`, { from, to }),
  deleteGameFile: (id: number, path: string) => request<void>('DELETE', `/games/${id}/files?path=${encodeURIComponent(path)}`),
  /** Sends a file as it is (not JSON), up to 50 GB (less if the game's disk limit leaves less room), telling onprogress how much has gone (0 to 1). Use lib/tasks.svelte.ts, which keeps uploads going across pages. */
  uploadGameFile: (id: number, path: string, file: File, onprogress?: (done: number) => void, signal?: AbortSignal) =>
    new Promise<void>((resolve, reject) => {
      if (DEMO) {
        // Pretend progress over a few seconds, so the demo shows uploads as they'd look.
        let done = 0;
        const timer = setInterval(() => {
          done = Math.min(1, done + 0.05);
          onprogress?.(done);
          if (done === 1) {
            clearInterval(timer);
            resolve();
          }
        }, 200);
        signal?.addEventListener('abort', () => {
          clearInterval(timer);
          reject(new ApiError('Cancelled', 0));
        });
        return;
      }
      // XMLHttpRequest rather than fetch: it reports an upload's progress.
      const xhr = new XMLHttpRequest();
      xhr.open('POST', `/api/games/${id}/files/upload?path=${encodeURIComponent(path)}`);
      xhr.upload.onprogress = (e) => e.lengthComputable && onprogress?.(e.loaded / e.total);
      xhr.onload = () => {
        if (xhr.status < 300) return resolve();
        let message = xhr.statusText;
        try {
          message = JSON.parse(xhr.responseText).error ?? message;
        } catch {
          // not JSON
        }
        reject(new ApiError(message, xhr.status));
      };
      xhr.onerror = () => reject(new ApiError('The upload was cut off', 0));
      xhr.onabort = () => reject(new ApiError('Cancelled', 0));
      signal?.addEventListener('abort', () => xhr.abort());
      xhr.send(file);
    }),
  /** Packs files and folders from one folder into a .tar.gz there. */
  archiveGameFiles: (id: number, path: string, paths: string[], to: string) => request<Job>('POST', `/games/${id}/files/archive`, { path, paths, to }),
  notifications: () => request<NotificationRule[]>('GET', '/notifications'),
  saveNotification: (n: Omit<NotificationRule, 'id'>, id?: number) =>
    id ? request<void>('PUT', `/notifications/${id}`, n) : request<void>('POST', '/notifications', n),
  deleteNotification: (id: number) => request<void>('DELETE', `/notifications/${id}`),
  testNotification: (id: number) => request<void>('POST', `/notifications/${id}/test`),
  /** Stops every download link to a backup that's been sent in a notification. */
  stopBackupLinks: (gameId: number, backupId: number) => request<void>('DELETE', `/games/${gameId}/backups/${backupId}/links`),
  backupStores: () => request<BackupStore[]>('GET', '/backup-stores'),
  /** Saves backup storage, after checking it can be written to. An empty secretKey keeps the one it has. */
  saveBackupStore: (s: Omit<BackupStore, 'id' | 'backups'>, id?: number) =>
    id ? request<void>('PUT', `/backup-stores/${id}`, s) : request<void>('POST', '/backup-stores', s),
  deleteBackupStore: (id: number) => request<void>('DELETE', `/backup-stores/${id}`),
  /** Where a game's new backups go: a backup storage's ID, or 0 for its own server. */
  setGameBackupStore: (id: number, storeId: number) => request<void>('PUT', `/games/${id}/backup-store`, { storeId }),
  /** Long jobs on the panel (archiving, unpacking, moving): the signed-in person's, running or just ended. */
  jobs: () => request<Job[]>('GET', '/jobs'),
  /** Stops a running job, or clears an ended one from the list. */
  stopJob: (id: string) => request<void>('DELETE', `/jobs/${id}`),
  /** Moves files and folders (names in the folder path) into the folder to. */
  moveGameFiles: (id: number, path: string, paths: string[], to: string) => request<Job>('POST', `/games/${id}/files/move`, { path, paths, to }),
  /** Unpacks a .zip, .tar.gz or .tar into the folder it's in. */
  unarchiveGameFile: (id: number, path: string) => request<Job>('POST', `/games/${id}/files/unarchive`, { path }),
  gameFileDownload: (id: number, path: string) => `/api/games/${id}/files/download?path=${encodeURIComponent(path)}`,
  gameStartup: (id: number) =>
    request<{ command: string; image: string; images: EggImage[]; variables: StartupVariable[] }>('GET', `/games/${id}/startup`),
  saveGameStartup: (id: number, values: Record<string, string>) => request<void>('PUT', `/games/${id}/startup`, values),
  gameSchedules: (id: number) => request<Schedule[]>('GET', `/games/${id}/schedules`),
  saveSchedule: (id: number, s: Pick<Schedule, 'name' | 'when' | 'action' | 'payload' | 'enabled'>, scheduleId?: number) =>
    request<void>(scheduleId ? 'PUT' : 'POST', `/games/${id}/schedules${scheduleId ? `/${scheduleId}` : ''}`, s),
  deleteSchedule: (id: number, scheduleId: number) => request<void>('DELETE', `/games/${id}/schedules/${scheduleId}`),
  runSchedule: (id: number, scheduleId: number) => request<void>('POST', `/games/${id}/schedules/${scheduleId}/run`),
  gameBackups: (id: number) => request<GameBackup[]>('GET', `/games/${id}/backups`),
  createGameBackup: (id: number, name = '') => request<GameBackup>('POST', `/games/${id}/backups`, { name }),
  lockGameBackup: (id: number, backupId: number, locked: boolean) => request<void>('PUT', `/games/${id}/backups/${backupId}`, { locked }),
  deleteGameBackup: (id: number, backupId: number) => request<void>('DELETE', `/games/${id}/backups/${backupId}`),
  restoreGameBackup: (id: number, backupId: number) => request<void>('POST', `/games/${id}/backups/${backupId}/restore`),
  gameBackupDownload: (id: number, backupId: number) => `/api/games/${id}/backups/${backupId}/download`,
  gameShares: (id: number) => request<GameShare[]>('GET', `/games/${id}/shares`),
  /** A one-time link for a new friend, or (with a username) shares it with a friend who has an account. */
  shareGame: (id: number, username = '') => request<{ link?: string }>('POST', `/games/${id}/shares`, username ? { username } : undefined),
  unshareGame: (id: number, userId: number) => request<void>('DELETE', `/games/${id}/shares/${userId}`),
  alerts: () => request<AlertsPage>('GET', '/alerts'),
  setAlertKinds: (kinds: string[]) => request<void>('PUT', '/alerts/kinds', { kinds }),
  addChannel: (kind: string, name: string, config: Record<string, string | number>) => request<void>('POST', '/alerts/channels', { kind, name, config }),
  deleteChannel: (id: number) => request<void>('DELETE', `/alerts/channels/${id}`),
  testChannel: (id: number) => request<void>('POST', `/alerts/channels/${id}/test`),
  npmInstances: () => request<NpmInstance[]>('GET', '/npm'),
  /** Links an NPM, signing in to it first to check the details. */
  /** With trustCertificate, an NPM with a self-signed certificate is linked trusting that one certificate. */
  linkNpm: (npm: { name: string; url: string; email: string; password: string; trustCertificate?: string }) => request<void>('POST', '/npm', npm),
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
  egg: (id: number) => request<EggDetail>('GET', `/eggs/${id}`),
  /** Imports a Pterodactyl egg from its JSON or a link to it. A name already here gives a 409 unless replace is set. */
  importEgg: (egg: { json?: string; url?: string; category: string; replace?: boolean }) => request<void>('POST', '/eggs', egg),
  deleteEgg: (id: number) => request<void>('DELETE', `/eggs/${id}`),
};
