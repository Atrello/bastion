// A small router: the current path as state, and navigate() to change it. Links are ordinary <a>
// elements; App.svelte turns clicks on them into navigate() calls, so the page doesn't reload.

let path = $state(location.pathname);

addEventListener('popstate', () => (path = location.pathname));

export const route = {
  get path() {
    return path;
  },
};

export function navigate(to: string) {
  if (to === location.pathname + location.search) return;
  history.pushState(null, '', to);
  path = new URL(to, location.origin).pathname;
  scrollTo(0, 0);
}

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
