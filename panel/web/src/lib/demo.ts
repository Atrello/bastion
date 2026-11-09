// A made-up homelab for `npm run demo`: four servers and their stacks, so the screens can be designed
// and screenshotted without a real panel. Never used in a build.
import type { ComposeVersion, Container, Server, Stack, StackFiles } from './types';

const GB = 1024 ** 3;
const now = Date.now();
const minutesAgo = (m: number) => new Date(now - m * 60_000).toISOString();

const servers: Server[] = [
  {
    id: 1, name: 'atlas', hostname: 'atlas', os: 'Debian GNU/Linux 12 (bookworm)', agentVersion: '0.1.0', online: true,
    lastSeen: minutesAgo(0), stacks: 4, containers: 11,
    metrics: { cpuPercent: 18, memUsed: 9.1 * GB, memTotal: 16 * GB, diskUsed: 312 * GB, diskTotal: 500 * GB, uptimeSecs: 1_209_600 },
  },
  {
    id: 2, name: 'forge', hostname: 'forge', os: 'Ubuntu 24.04.1 LTS', agentVersion: '0.1.0', online: true,
    lastSeen: minutesAgo(0), stacks: 2, containers: 3,
    metrics: { cpuPercent: 46, memUsed: 27.4 * GB, memTotal: 32 * GB, diskUsed: 640 * GB, diskTotal: 1863 * GB, uptimeSecs: 273_600 },
  },
  {
    id: 3, name: 'vault', hostname: 'vault', os: 'Debian GNU/Linux 12 (bookworm)', agentVersion: '0.1.0', online: true,
    lastSeen: minutesAgo(0), stacks: 3, containers: 7,
    metrics: { cpuPercent: 7, memUsed: 3.2 * GB, memTotal: 8 * GB, diskUsed: 14_100 * GB, diskTotal: 24_000 * GB, uptimeSecs: 4_060_800 },
  },
  {
    id: 4, name: 'edge-vps', hostname: 'edge-vps', os: 'Debian GNU/Linux 12 (bookworm)', agentVersion: '0.1.0', online: false,
    lastSeen: minutesAgo(26), stacks: 2, containers: 2, metrics: null,
  },
];

const stack = (serverId: number, name: string, running: number, total: number, hasEnv = true): Stack => ({
  serverId, serverName: servers[serverId - 1].name, name, running, total, hasEnv,
  state: total === 0 ? 'new' : running === total ? 'running' : running === 0 ? 'stopped' : 'partial',
  configFile: `/opt/stacks/${name}/compose.yaml`,
});

const stacks: Stack[] = [
  stack(1, 'home-assistant', 3, 3),
  stack(1, 'paperless', 3, 4),
  stack(1, 'nginx-proxy-manager', 2, 2),
  stack(1, 'monitoring', 2, 2, false),
  stack(2, 'minecraft', 1, 1),
  stack(2, 'valheim', 0, 2),
  stack(3, 'immich', 4, 4),
  stack(3, 'jellyfin', 1, 1, false),
  stack(3, 'backups', 2, 2),
  stack(4, 'vaultwarden', 1, 1),
  stack(4, 'uptime-kuma', 0, 1, false),
];

const containers: Container[] = [
  ['immich_server', 'ghcr.io/immich-app/immich-server:v1.133.0', 'running', 'Up 3 days', '0.0.0.0:2283->2283/tcp', 'immich', 'immich-server'],
  ['immich_machine_learning', 'ghcr.io/immich-app/immich-machine-learning:v1.133.0', 'running', 'Up 3 days', '', 'immich', 'immich-machine-learning'],
  ['immich_redis', 'redis:7.4-alpine', 'running', 'Up 3 days (healthy)', '', 'immich', 'redis'],
  ['immich_postgres', 'tensorchord/pgvecto-rs:pg16-v0.3.0', 'running', 'Up 3 days (healthy)', '', 'immich', 'database'],
  ['paperless-webserver-1', 'ghcr.io/paperless-ngx/paperless-ngx:2.15', 'running', 'Up 5 hours', '0.0.0.0:8000->8000/tcp', 'paperless', 'webserver'],
  ['paperless-worker-1', 'ghcr.io/paperless-ngx/paperless-ngx:2.15', 'exited', 'Exited (1) 12 minutes ago', '', 'paperless', 'worker'],
  ['paperless-db-1', 'postgres:16', 'running', 'Up 5 hours', '', 'paperless', 'db'],
  ['paperless-broker-1', 'redis:7', 'running', 'Up 5 hours', '', 'paperless', 'broker'],
].map(([name, image, state, status, ports, stackName, service], i) => ({
  id: (0xa3f9c2 + i * 7919).toString(16).padStart(12, '0'),
  serverId: stackName === 'immich' ? 3 : 1,
  name, image, status, ports, stack: stackName, service,
  state: state as Container['state'],
}));

const files: StackFiles = {
  compose: `services:
  immich-server:
    container_name: immich_server
    image: ghcr.io/immich-app/immich-server:\${IMMICH_VERSION:-release}
    volumes:
      - \${UPLOAD_LOCATION}:/usr/src/app/upload
      - /etc/localtime:/etc/localtime:ro
    env_file:
      - .env
    ports:
      - "2283:2283"
    depends_on:
      - redis
      - database
    restart: always

  redis:
    container_name: immich_redis
    image: redis:7.4-alpine
    restart: always

  database:
    container_name: immich_postgres
    image: tensorchord/pgvecto-rs:pg16-v0.3.0
    environment:
      POSTGRES_PASSWORD: \${DB_PASSWORD}
      POSTGRES_USER: \${DB_USERNAME}
      POSTGRES_DB: \${DB_DATABASE_NAME}
    volumes:
      - \${DB_DATA_LOCATION}:/var/lib/postgresql/data
    restart: always
`,
  env: `# Where uploaded photos and videos are kept
UPLOAD_LOCATION=/mnt/tank/photos
DB_DATA_LOCATION=./postgres

IMMICH_VERSION=v1.133.0

DB_PASSWORD=change-me
DB_USERNAME=postgres
DB_DATABASE_NAME=immich
`,
};

const history: ComposeVersion[] = [
  { id: 12, savedAt: minutesAgo(60 * 26), savedBy: 'Robin Hale', note: 'Pinned IMMICH_VERSION' },
  { id: 11, savedAt: minutesAgo(60 * 24 * 9), savedBy: 'Robin Hale', note: '' },
  { id: 10, savedAt: minutesAgo(60 * 24 * 30), savedBy: 'Robin Hale', note: 'Moved uploads to the tank' },
];

const logs = [
  'immich_server   | [Nest] 7  - 11/09/2026, 18:41:02     LOG [Api:Bootstrap] Immich Server is listening on http://[::1]:2283',
  'immich_server   | [Nest] 7  - 11/09/2026, 18:41:02     LOG [Api:Bootstrap] Version: v1.133.0',
  'immich_redis    | 1:M 09 Nov 2026 18:40:58.114 * Ready to accept connections tcp',
  'immich_postgres | 2026-11-09 18:40:59.208 UTC [1] LOG:  database system is ready to accept connections',
  'immich_server   | [Nest] 7  - 11/09/2026, 18:44:12     LOG [Microservices:JobService] Thumbnail generation: 214 queued',
  'immich_server   | [Nest] 7  - 11/09/2026, 18:46:30     WARN [Microservices:MetadataService] No exif data for /mnt/tank/photos/IMG_2291.HEIC',
  'immich_server   | [Nest] 7  - 11/09/2026, 18:47:05     LOG [Microservices:JobService] Thumbnail generation: done (214)',
];

export function demo(method: string, path: string, body?: unknown): unknown {
  if (method === 'GET' && path === '/servers') return servers;
  if (method === 'GET' && path === '/stacks') return stacks;
  if (method === 'GET' && path === '/containers') return containers;
  if (method === 'GET' && path.endsWith('/files')) return files;
  if (method === 'GET' && path.endsWith('/history')) return history;
  if (method === 'GET' && path.includes('/logs')) return { lines: logs };
  if (method === 'POST' && path === '/servers') {
    const name = (body as { name: string }).name;
    return {
      server: { ...servers[0], id: 99, name, online: false, metrics: null, stacks: 0, containers: 0 },
      joinCommand: 'curl -fsSL https://bastion.home.lab/install.sh | sudo sh -s -- 8f3c2a1d-join-token',
    };
  }
  return { output: '' };
}
