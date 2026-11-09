// Uploads to game servers, kept here rather than in the file manager so they carry on when you move to
// another page. One file goes at a time; components/Uploads.svelte shows them on every page.
import { api } from './api';

export type Upload = {
  key: number;
  gameId: number;
  gameName: string;
  /** Where it's going, like "/plugins/worldedit.jar". */
  path: string;
  name: string;
  /** How much has gone, 0 to 1. */
  done: number;
  state: 'waiting' | 'sending' | 'done' | 'failed' | 'cancelled';
  error?: string;
};

export const uploads = $state<Upload[]>([]);
/** Counts finished uploads per game, so its file list can refresh when one lands. */
export const landed = $state<Record<number, number>>({});

const files = new Map<number, File>();
const stops = new Map<number, AbortController>();
let next = 1;
let running = false;

export const busy = () => uploads.some((u) => u.state === 'waiting' || u.state === 'sending');

/** Queues files to upload into a folder ("/plugins/") of a game. */
export function startUploads(gameId: number, gameName: string, folder: string, chosen: File[]) {
  for (const file of chosen) {
    const key = next++;
    files.set(key, file);
    uploads.push({ key, gameId, gameName, path: folder + file.name, name: file.name, done: 0, state: 'waiting' });
  }
  void run();
}

export function cancel(key: number) {
  const u = uploads.find((u) => u.key === key);
  if (u?.state === 'waiting') {
    u.state = 'cancelled';
    files.delete(key);
  }
  stops.get(key)?.abort();
}

/** Clears finished, failed and cancelled uploads from the list. */
export function clearFinished() {
  for (let i = uploads.length - 1; i >= 0; i--) if (uploads[i].state !== 'waiting' && uploads[i].state !== 'sending') uploads.splice(i, 1);
}

async function run() {
  if (running) return;
  running = true;
  for (let u = uploads.find((u) => u.state === 'waiting'); u; u = uploads.find((u) => u.state === 'waiting')) {
    const file = files.get(u.key)!;
    const stop = new AbortController();
    stops.set(u.key, stop);
    u.state = 'sending';
    try {
      await api.uploadGameFile(u.gameId, u.path, file, (done) => (u.done = done), stop.signal);
      u.done = 1;
      u.state = 'done';
      landed[u.gameId] = (landed[u.gameId] ?? 0) + 1;
    } catch (err) {
      u.state = stop.signal.aborted ? 'cancelled' : 'failed';
      u.error = (err as Error).message;
    }
    stops.delete(u.key);
    files.delete(u.key);
  }
  running = false;
}

// Closing the tab or reloading would cut an upload off: the browser asks first. (Its own wording; a page
// can't put this in its own dialog.)
addEventListener('beforeunload', (e) => {
  if (busy()) e.preventDefault();
});
