// The shapes the panel's API returns. docs/api.md describes each endpoint.

export type Metrics = {
  cpuPercent: number;
  memUsed: number;
  memTotal: number;
  diskUsed: number;
  diskTotal: number;
  uptimeSecs: number;
};

export type Server = {
  id: number;
  name: string;
  hostname: string;
  /** Its address on its network, as the agent sees it; empty if it couldn't tell. */
  address: string;
  os: string;
  arch: string;
  cpus: number;
  docker: string;
  agentVersion: string;
  online: boolean;
  /** When the agent last said anything, as an ISO time. */
  lastSeen: string;
  metrics: Metrics | null;
  stacks: number;
  containers: number;
  games: number;
};

export type StackState = 'running' | 'partial' | 'stopped' | 'new';

export type Stack = {
  serverId: number;
  serverName: string;
  name: string;
  state: StackState;
  running: number;
  total: number;
  configFile: string;
  hasEnv: boolean;
  /** How many of its containers have a newer image in their registry. */
  updates: number;
};

export type Container = {
  id: string;
  serverId: number;
  name: string;
  image: string;
  state: 'running' | 'exited' | 'restarting' | 'paused' | 'created' | 'dead';
  status: string;
  ports: string;
  stack: string;
  service: string;
  /** The registry has a newer image for its tag. */
  update: boolean;
};

export type StackFiles = {
  compose: string;
  /** The .env beside the compose file, as it is on the server; null if there isn't one. */
  env: string | null;
};

export type GitStack = {
  repo: string;
  branch: string;
  path: string;
  autoDeploy: boolean;
  publicKey: string;
  deployedCommit: string;
  remoteCommit: string;
  checkedAt: string | null;
  error: string;
};

export type ComposeVersion = { id: number; savedAt: string; savedBy: string; note: string };

/** A game server, run from an egg (Pterodactyl's game definitions) as a container on one of the servers. */
export type GameServer = {
  id: number;
  name: string;
  egg: string;
  eggId: number;
  serverId: number;
  serverName: string;
  state: 'starting' | 'running' | 'stopping' | 'stopped' | 'crashed' | 'installing' | 'install-failed' | 'offline';
  /** Where players connect, e.g. "192.168.1.11:25565". */
  address: string;
  players: { online: number; max: number } | null;
  image: string;
  ports: number[];
  cpuPercent: number;
  /** 100 is one core; 0 is no limit. */
  cpuLimit: number;
  memUsed: number;
  memLimit: number;
  diskUsed: number;
  /** 0 is no limit. */
  diskLimit: number;
  uptimeSecs: number;
  /** Typed into its console before a stop, with %s for the message; empty when it can't warn players. */
  warnCommand: string;
  /** The viewer is the panel's owner, who can change its limits, delete and share it. */
  owner: boolean;
};

export type Egg = {
  id: number;
  name: string;
  /** What kind of game: Minecraft, Steam, Voice and the like, chosen when it's imported. */
  category: string;
  author: string;
  description: string;
  servers: number;
};

export type EggImage = { label: string; image: string };

/** What the new game server form needs from an egg. */
export type EggDetail = { name: string; images: EggImage[]; startup: string };

/** A setting the egg lets each game server choose, like the version or the world's seed. */
export type StartupVariable = {
  key: string;
  name: string;
  description: string;
  value: string;
  /** Laravel-style rules from the egg, like "required|string|max:20". */
  rules: string;
  /** Friends can only change the ones the egg lets players change. */
  editable: boolean;
};

export type GameFile = { name: string; dir: boolean; size: number; modified: string };

export type Schedule = {
  id: number;
  name: string;
  /** A cron expression: minute, hour, day, month, weekday. */
  when: string;
  action: 'restart' | 'command' | 'backup';
  payload: string;
  enabled: boolean;
  nextRun: string;
  lastRunAt: string;
  lastError: string;
};

export type GameBackup = { id: number; name: string; size: number; createdAt: string; locked: boolean; state: 'making' | 'done' | 'failed' };

export type GameShare = { userId: number; username: string };

export type Volume = { name: string; driver: string; size: number; containers: number; usedBy: string[] | null; stack: string };
export type Image = { id: string; repository: string; tag: string; size: number; uniqueSize: number; containers: number; created: string };
export type CleanupItem = { name: string; size: number };
export type Cleanup = {
  images: CleanupItem[] | null;
  imagesSize: number;
  volumes: CleanupItem[] | null;
  volumesSize: number;
  buildCacheSize: number;
};

/** A linked Nginx Proxy Manager. */
/** A linked Nginx Proxy Manager: npm, or npmplus (a fork with its own fields). */
export type NpmInstance = { id: number; name: string; url: string; email: string; flavour: 'npm' | 'npmplus' };

/** The kinds of thing NPM manages, as its API names them. */
export type NpmKind = 'proxy-hosts' | 'redirection-hosts' | 'streams' | 'dead-hosts' | 'access-lists' | 'certificates';

/** An NPM item exactly as NPM's API has it (docs/api.md links its fields). */
export type NpmItem = Record<string, any> & { id: number };

/** The panel's last check of a proxy host: the HTTP status it answered with, or 0 if it didn't. */
export type HostHealth = { status: number; ms: number; error?: string; checkedAt: string };

export type NpmSummary = { hosts: number; up: number; problems: { kind: string; title: string; detail: string }[] };
