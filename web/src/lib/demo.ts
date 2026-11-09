// A made-up homelab for `npm run demo`: four servers and their stacks, so the screens can be designed
// and screenshotted without a real panel. Never used in a build.
import type {
  ComposeVersion,
  Container,
  Egg,
  GameBackup,
  GameFile,
  GameServer,
  Schedule,
  Server,
  Stack,
  StackFiles,
  StartupVariable,
} from './types';

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
  updates: name === 'immich' ? 2 : name === 'home-assistant' ? 1 : 0,
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
  update: name === 'immich_server' || name === 'immich_machine_learning',
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


const MB = 1024 ** 2;

const games: GameServer[] = [
  {
    id: 1, name: 'Survival SMP', egg: 'Paper', serverId: 2, serverName: 'forge', state: 'running',
    address: 'mc.example.co.uk:25565', players: { online: 7, max: 20 },
    cpuPercent: 38, memUsed: 6.2 * GB, memLimit: 8 * GB, diskUsed: 4.2 * GB, uptimeSecs: 273_600,
  },
  {
    id: 2, name: 'Valheim', egg: 'Valheim (BepInEx)', serverId: 2, serverName: 'forge', state: 'running',
    address: 'forge.home.lab:2456', players: { online: 2, max: 10 },
    cpuPercent: 21, memUsed: 3.1 * GB, memLimit: 6 * GB, diskUsed: 1.9 * GB, uptimeSecs: 86_400,
  },
  {
    id: 3, name: 'Satisfactory', egg: 'Satisfactory', serverId: 2, serverName: 'forge', state: 'running',
    address: 'forge.home.lab:7777', players: { online: 0, max: 4 },
    cpuPercent: 12, memUsed: 9.8 * GB, memLimit: 12 * GB, diskUsed: 11 * GB, uptimeSecs: 43_200,
  },
  {
    id: 4, name: 'Creative', egg: 'Fabric', serverId: 2, serverName: 'forge', state: 'crashed',
    address: 'forge.home.lab:25566', players: null,
    cpuPercent: 0, memUsed: 0, memLimit: 4 * GB, diskUsed: 1.1 * GB, uptimeSecs: 0,
  },
  {
    id: 5, name: 'CS2 retakes', egg: 'Counter-Strike 2', serverId: 1, serverName: 'atlas', state: 'stopped',
    address: 'atlas.home.lab:27015', players: null,
    cpuPercent: 0, memUsed: 0, memLimit: 4 * GB, diskUsed: 38 * GB, uptimeSecs: 0,
  },
  {
    id: 6, name: 'TeamSpeak', egg: 'TeamSpeak 3', serverId: 4, serverName: 'edge-vps', state: 'running',
    address: 'ts.example.co.uk:9987', players: { online: 3, max: 32 },
    cpuPercent: 1, memUsed: 48 * MB, memLimit: 256 * MB, diskUsed: 22 * MB, uptimeSecs: 1_728_000,
  },
];

const eggs: Egg[] = [
  { id: 1, name: 'Paper', category: 'Minecraft', author: 'parker@pterodactyl.io', description: 'High performance Spigot fork that aims to fix gameplay and mechanics inconsistencies.', servers: 1 },
  { id: 2, name: 'Fabric', category: 'Minecraft', author: 'community', description: 'Lightweight, modular mod loader for Minecraft.', servers: 1 },
  { id: 3, name: 'Vanilla Minecraft', category: 'Minecraft', author: 'support@pterodactyl.io', description: 'The official Minecraft server, as Mojang makes it.', servers: 0 },
  { id: 4, name: 'Valheim (BepInEx)', category: 'Steam', author: 'community', description: 'Valheim dedicated server with the BepInEx mod loader.', servers: 1 },
  { id: 5, name: 'Satisfactory', category: 'Steam', author: 'community', description: 'Satisfactory dedicated server, installed through SteamCMD.', servers: 1 },
  { id: 6, name: 'Counter-Strike 2', category: 'Steam', author: 'community', description: 'CS2 dedicated server, installed through SteamCMD.', servers: 1 },
  { id: 7, name: 'TeamSpeak 3', category: 'Voice', author: 'support@pterodactyl.io', description: 'VoIP software designed with security in mind.', servers: 1 },
];

const gameConsole = [
  '[18:41:02 INFO]: Alex joined the game',
  '[18:41:15 INFO]: <Alex> anyone up for the nether?',
  '[18:42:30 INFO]: Saving the game (this may take a moment!)',
  '[18:42:31 INFO]: Saved the game',
  '[18:43:07 INFO]: Jane Cooper has made the advancement [Diamonds!]',
  '[18:43:55 WARN]: Can\'t keep up! Is the server overloaded? Running 2104ms or 42 ticks behind',
  '[18:44:12 INFO]: [Bastion] Backup in 15 minutes: world (4.2 GB)',
  '[18:44:40 INFO]: There are 7 of a max of 20 players online: Alex, Jane Cooper, Robin, Casey, Sam, Morgan, Ellis',
];

const gameFiles: GameFile[] = [
  { name: 'cache', dir: true, size: 0, modified: minutesAgo(60 * 70) },
  { name: 'logs', dir: true, size: 0, modified: minutesAgo(3) },
  { name: 'plugins', dir: true, size: 0, modified: minutesAgo(60 * 50) },
  { name: 'world', dir: true, size: 0, modified: minutesAgo(1) },
  { name: 'world_nether', dir: true, size: 0, modified: minutesAgo(1) },
  { name: 'world_the_end', dir: true, size: 0, modified: minutesAgo(1) },
  { name: 'banned-players.json', dir: false, size: 2, modified: minutesAgo(60 * 24 * 12) },
  { name: 'bukkit.yml', dir: false, size: 1_241, modified: minutesAgo(60 * 24 * 12) },
  { name: 'eula.txt', dir: false, size: 158, modified: minutesAgo(60 * 24 * 40) },
  { name: 'ops.json', dir: false, size: 214, modified: minutesAgo(60 * 24 * 3) },
  { name: 'paper.jar', dir: false, size: 51_380_224, modified: minutesAgo(60 * 76) },
  { name: 'server.properties', dir: false, size: 1_489, modified: minutesAgo(60 * 26) },
  { name: 'whitelist.json', dir: false, size: 812, modified: minutesAgo(60 * 24 * 3) },
];

const startup: { command: string; image: string; variables: StartupVariable[] } = {
  command: 'java -Xms128M -XX:MaxRAMPercentage=95.0 -Dterminal.jline=false -jar {{SERVER_JARFILE}}',
  image: 'ghcr.io/pterodactyl/yolks:java_21',
  variables: [
    { key: 'MINECRAFT_VERSION', name: 'Minecraft version', description: 'The version to run, or "latest".', value: '1.21.4', editable: true },
    { key: 'BUILD_NUMBER', name: 'Build number', description: 'The Paper build to use, or "latest".', value: 'latest', editable: true },
    { key: 'SERVER_JARFILE', name: 'Server jar file', description: 'The name of the jar to run.', value: 'paper.jar', editable: true },
    { key: 'EULA', name: 'Accept the EULA', description: 'Mojang\'s EULA must be accepted to run a server.', value: 'true', editable: false },
  ],
};

const schedules: Schedule[] = [
  { id: 1, name: 'Nightly restart', when: 'Every day at 05:00', action: 'Warn players, then restart', nextRun: new Date(now + 10 * 3600_000).toISOString(), enabled: true },
  { id: 2, name: 'World backup', when: 'Every day at 03:00', action: 'Back up world, keep 7', nextRun: new Date(now + 8 * 3600_000).toISOString(), enabled: true },
  { id: 3, name: 'Weekend whitelist off', when: 'Fridays at 18:00', action: 'Run: whitelist off', nextRun: new Date(now + 4 * 86400_000).toISOString(), enabled: false },
];

const backups: GameBackup[] = [
  { id: 41, name: 'Nightly', size: 4.1 * GB, createdAt: minutesAgo(60 * 16), locked: false },
  { id: 40, name: 'Before 1.21.4 update', size: 3.9 * GB, createdAt: minutesAgo(60 * 76), locked: true },
  { id: 39, name: 'Nightly', size: 3.9 * GB, createdAt: minutesAgo(60 * 40), locked: false },
];

export function demo(method: string, path: string, body?: unknown): unknown {
  if (path === '/auth/state') return { setupNeeded: false, signedIn: true, username: 'robin', passkeys: true };
  if (path === '/account')
    return {
      username: 'robin',
      hasAuthenticator: true,
      recoveryCodesLeft: 9,
      passkeys: [
        { id: 1, name: 'Laptop', createdAt: minutesAgo(60 * 24 * 20), lastUsedAt: minutesAgo(40) },
        { id: 2, name: 'Phone', createdAt: minutesAgo(60 * 24 * 6), lastUsedAt: minutesAgo(60 * 24 * 2) },
      ],
    };
  if (method === 'GET' && path === '/servers') return servers;
  if (method === 'GET' && path === '/stacks') return stacks;
  if (method === 'GET' && path === '/containers') return containers;
  if (method === 'GET' && path.endsWith('/files')) return files;
  if (method === 'GET' && path.endsWith('/history')) return history;
  if (method === 'GET' && path.endsWith('/git'))
    return path.includes('/immich/')
      ? {
          repo: 'git@github.com:robin/homelab.git', branch: 'main', path: 'stacks/immich/compose.yaml', autoDeploy: false,
          publicKey: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIK0n8yq3Xc1Hq6c4mJpS1fE2xYbQe9Zt7uLwV5rN3aP bastion-immich',
          deployedCommit: '3f9c2a1e7b4d', remoteCommit: '8a1d4c7e2f90', checkedAt: minutesAgo(3), error: '',
        }
      : null;
  if (method === 'GET' && path.includes('/logs')) return { lines: logs };
  if (method === 'GET' && path.endsWith('/volumes'))
    return [
      { name: 'immich_pgdata', driver: 'local', size: 2.1 * GB, containers: 1, usedBy: ['immich_postgres'], stack: 'immich' },
      { name: 'immich_model-cache', driver: 'local', size: 1.4 * GB, containers: 1, usedBy: ['immich_machine_learning'], stack: 'immich' },
      { name: 'old-nextcloud_data', driver: 'local', size: 18.3 * GB, containers: 0, usedBy: null, stack: 'old-nextcloud' },
    ];
  if (method === 'GET' && path.endsWith('/images'))
    return [
      { id: 'a1b2c3d4e5f6', repository: 'ghcr.io/immich-app/immich-server', tag: 'v1.133.0', size: 1.3 * GB, uniqueSize: 1.1 * GB, containers: 1, created: '2 weeks ago' },
      { id: 'b2c3d4e5f6a1', repository: 'ghcr.io/immich-app/immich-server', tag: 'v1.132.3', size: 1.3 * GB, uniqueSize: 0.4 * GB, containers: 0, created: '5 weeks ago' },
      { id: 'c3d4e5f6a1b2', repository: 'redis', tag: '7.4-alpine', size: 41 * MB, uniqueSize: 41 * MB, containers: 1, created: '2 months ago' },
    ];
  if (method === 'GET' && path.endsWith('/cleanup'))
    return {
      images: [{ name: 'ghcr.io/immich-app/immich-server:v1.132.3', size: 1.3 * GB }],
      imagesSize: 0.4 * GB,
      volumes: [{ name: 'old-nextcloud_data', size: 18.3 * GB }],
      volumesSize: 18.3 * GB,
      buildCacheSize: 640 * MB,
    };
  if (method === 'GET' && path === '/alerts')
    return {
      channels: [{ id: 1, kind: 'ntfy', name: 'Phone', detail: 'https://ntfy.sh/robin-homelab' }],
      kinds: ['offline', 'crash', 'unhealthy', 'disk'],
      recent: [
        { id: 3, kind: 'crash', title: 'paperless-worker-1 keeps restarting', detail: 'On atlas: Restarting (1) 12 seconds ago.', raisedAt: minutesAgo(12), clearedAt: null },
        { id: 2, kind: 'offline', title: 'edge-vps is offline', detail: 'Its agent hasn’t been connected since 18:31.', raisedAt: minutesAgo(24), clearedAt: null },
        { id: 1, kind: 'disk', title: 'vault’s disk is 91% full', detail: 'Clean up unused images and volumes.', raisedAt: minutesAgo(60 * 30), clearedAt: minutesAgo(60 * 29) },
      ],
    };
  if (method === 'GET' && path === '/games') return games;
  if (method === 'GET' && path === '/eggs') return eggs;
  const game = /^\/games\/(\d+)(\/[a-z]+)?/.exec(path);
  if (method === 'GET' && game) {
    const part = game[2] ?? '';
    if (part === '/console') return { lines: gameConsole };
    if (part === '/files') return gameFiles;
    if (part === '/startup') return startup;
    if (part === '/schedules') return schedules;
    if (part === '/backups') return backups;
    return games.find((g) => g.id === Number(game[1]));
  }
  if (method === 'POST' && path === '/servers') {
    const name = (body as { name: string }).name;
    return {
      server: { ...servers[0], id: 99, name, online: false, metrics: null, stacks: 0, containers: 0 },
      joinCommand: 'curl -fsSL https://bastion.home.lab/install.sh | sudo sh -s -- 8f3c2a1d-join-token',
    };
  }
  return { output: '' };
}
