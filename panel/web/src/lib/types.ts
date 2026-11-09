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
};

export type StackFiles = {
  compose: string;
  /** The .env beside the compose file, as it is on the server; null if there isn't one. */
  env: string | null;
};

export type ComposeVersion = { id: number; savedAt: string; savedBy: string; note: string };
