// Things that take a while, shown in the corner of every page (components/Tasks.svelte) with their
// progress, and stoppable from there:
//  - uploads to game servers, which the browser sends, one file at a time. Kept here rather than in the
//    file manager so they carry on when you move to another page (but not if you close the tab);
//  - jobs the panel runs on a server (archiving, unpacking, moving files: internal/panel/api/jobs.go),
//    which carry on whatever the browser does. They're checked for every second or so while any runs.
import { api } from './api';
import type { Job } from './types';

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
/** Counts finished uploads and jobs per game, so its file list can refresh when one lands. */
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

/** Clears finished, failed and cancelled uploads and jobs from the list. */
export function clearFinished() {
  for (let i = uploads.length - 1; i >= 0; i--) if (uploads[i].state !== 'waiting' && uploads[i].state !== 'sending') uploads.splice(i, 1);
  for (const j of jobs.filter((j) => !runningJob(j))) void api.stopJob(j.id).catch(() => {});
  for (let i = jobs.length - 1; i >= 0; i--) if (!runningJob(jobs[i])) jobs.splice(i, 1);
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

export const jobs = $state<Job[]>([]);
let watching = false;

const runningJob = (j: Job) => j.state === 'running' || j.state === 'stopping';

/** Shows a job just started, and follows it until it ends. */
export function followJob(job: Job) {
  jobs.push(job);
  void watchJobs();
}

/** Picks up the panel's jobs (after signing in, or a reload) and follows any running. */
export async function watchJobs() {
  if (watching) return;
  watching = true;
  try {
    for (;;) {
      let latest: Job[];
      try {
        latest = await api.jobs();
      } catch {
        latest = jobs; // signed out, or the panel's restarting: keep what's shown, try again
      }
      for (const j of latest) {
        const was = jobs.find((k) => k.id === j.id);
        if (was && runningJob(was) && j.state === 'done') landed[j.gameId] = (landed[j.gameId] ?? 0) + 1;
      }
      jobs.splice(0, jobs.length, ...latest);
      if (!jobs.some(runningJob)) break;
      await new Promise((r) => setTimeout(r, 1200));
    }
  } finally {
    watching = false;
  }
}

export async function stopJob(id: string) {
  const j = jobs.find((k) => k.id === id);
  if (j?.state === 'running') j.state = 'stopping';
  try {
    await api.stopJob(id);
  } catch {
    // it had ended already
  }
  void watchJobs();
}

// Closing the tab or reloading would cut an upload off (jobs carry on): the browser asks first. (Its own wording; a page
// can't put this in its own dialog.)
addEventListener('beforeunload', (e) => {
  if (busy()) e.preventDefault();
});
