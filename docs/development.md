# Developing Bastion

## Building and checking

Requires Go (the version in `go.mod`; Go fetches it if yours is older) and Node.js 22.

```sh
make            # builds the web app, then the panel into bin/bastion
make test       # Go tests
make check      # everything a change must pass: tests, vet, formatting, govulncheck, npm audit, type check
BASTION_TEST_DOCKER=1 go test ./internal/agent   # also the tests against real Docker (stack backups)
```

The web app's demo, on made-up data: `npm run demo` in `web` (for a browser), or `npm run build:demo` for
the copy published on GitHub Pages (`.github/workflows/demo.yml`).

Run the panel locally:

```sh
BASTION_PUBLIC_URL=http://localhost:8080 ./bin/bastion
```

The first time, its log prints a one-time setup link to make your account. Use `localhost`, not
`127.0.0.1`: passkeys need a name.

## The panel's server

Go, in `cmd/bastion` and `internal/panel`:

| Package | What it does |
|---|---|
| `config` | Settings from the environment |
| `store` | The SQLite database and its migrations (`store/migrations`, numbered, run once each) |
| `auth` | Passwords, authenticator and recovery codes, passkeys, sessions, sign-in limits, first-run setup |
| `api` | The HTTP API: routing, the same-origin check, JSON in and out, and the handlers |
| `server` | Security headers, logging, and the API and web app on one address |

The web app is built into the binary (`web/web.go`), so the panel is one file to run.

Tests are beside the code (`*_test.go`). The API's tests drive a real panel in memory the way a browser
would, including trying what an attacker would: a second setup, a reused code, a cross-site request.

## The web app

The panel's web app is in `web`: Svelte 5 with TypeScript, built by Vite.

```sh
cd web
npm install
npm run demo     # the app on demo data (src/lib/demo.ts), no panel needed
npm run dev      # the app against a panel on :8080 (Vite passes /api through)
npm run check    # type-check every file
npm run build    # the production build, into dist/
```

### How it's put together

- **Sections**: the overview and servers, then Docker (stacks and containers) and Games (game servers and
  eggs), each with its own heading in the menu (`src/components/Sidebar.svelte`).
- **Pages** (`src/pages`) are one file each, picked by `App.svelte` from the address. The router
  (`src/lib/router.svelte.ts`) is a few lines: links are ordinary `<a>` elements, and clicks on them are
  turned into page changes without a reload.
- **Data** comes from `src/lib/api.ts`, one function per endpoint. A page loads what it needs with
  `load()` (`src/lib/load.svelte.ts`), which gives it `data`, `error` and `reload()`.
- **Demo mode** (`npm run demo`) answers every API call from `src/lib/demo.ts` instead. It's only loaded
  in demo mode, so it's never part of a real panel.
- **Styles**: shared ones (colours, buttons, tables, cards) are in `src/app.css`; anything else lives in
  the component that uses it.

### Keeping it light

- No UI library and no router dependency: the panel's own code is about 26 KB gzipped.
- The code editor (CodeMirror, about 140 KB gzipped) is the one large dependency. It's loaded only when a
  stack's page is opened.
- Plain system fonts, one dark theme, no icon font: icons are small inline SVGs.

### Conventions

- Plain British English on screen, in comments and in the docs. Short sentences, sentence case.
- Each file starts with a comment saying what it's for.
- Names in demo data are made up.
