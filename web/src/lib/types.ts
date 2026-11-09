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
  os: string;
  agentVersion: string;
  online: boolean;
  /** When the agent last said anything, as an ISO time. */
  lastSeen: string;
  metrics: Metrics | null;
  stacks: number;
  containers: number;
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
  serverId: number;
  serverName: string;
  state: 'running' | 'starting' | 'stopping' | 'stopped' | 'crashed';
  /** Where players connect, e.g. "mc.example.co.uk:25565". */
  address: string;
  players: { online: number; max: number } | null;
  cpuPercent: number;
  memUsed: number;
  memLimit: number;
  diskUsed: number;
  uptimeSecs: number;
};

export type Egg = {
  id: number;
  name: string;
  /** What kind of game, from the egg's nest: Minecraft, Steam, Voice and the like. */
  category: string;
  author: string;
  description: string;
  servers: number;
};

/** A setting the egg lets each game server choose, like the version or the world's seed. */
export type StartupVariable = {
  key: string;
  name: string;
  description: string;
  value: string;
  /** Changing it needs a restart, and some can only be changed by an admin. */
  editable: boolean;
};

export type GameFile = { name: string; dir: boolean; size: number; modified: string };

export type Schedule = { id: number; name: string; when: string; action: string; nextRun: string; enabled: boolean };

export type GameBackup = { id: number; name: string; size: number; createdAt: string; locked: boolean };

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
