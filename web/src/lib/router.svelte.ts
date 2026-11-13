// A small router: the current path as state, and navigate() to change it. Links are ordinary <a>
// elements; App.svelte turns clicks on them into navigate() calls, so the page doesn't reload.
// Paths are the app's own ("/servers"): served under a folder (the demo on GitHub Pages is under
// /bastion/), the folder is added to the address bar and taken off what the app sees.

const base = import.meta.env.BASE_URL.replace(/\/$/, '');

/** The app's path for the address bar's. */
const inApp = (pathname: string) => (base && pathname.startsWith(base) ? pathname.slice(base.length) || '/' : pathname);

let path = $state(inApp(location.pathname));

addEventListener('popstate', () => (path = inApp(location.pathname)));

export const route = {
  get path() {
    return path;
  },
};

export function navigate(to: string) {
  const url = new URL(to, location.origin);
  const app = inApp(url.pathname);
  if (base + app + url.search === location.pathname + location.search) return;
  history.pushState(null, '', base + app + url.search);
  path = app;
  scrollTo(0, 0);
}

/** An asset served beside the app, like its icon. */
export const asset = (file: string) => `${base}/${file}`;

/** Matches a path against a pattern like "/stacks/:server/:name", giving the named parts. */
export function match(pattern: string, against = path): Record<string, string> | null {
  const want = pattern.split('/');
  const got = against.split('/');
  if (want.length !== got.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < want.length; i++) {
    if (want[i].startsWith(':')) params[want[i].slice(1)] = decodeURIComponent(got[i]);
    else if (want[i] !== got[i]) return null;
  }
  return params;
}
