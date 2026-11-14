# Desktop wallet screens

This folder holds the screens of the Windows desktop wallet (`cvn_node.exe`). They are written with [Svelte](https://svelte.dev) and built with [Vite](https://vite.dev).

| Path | What it is |
|---|---|
| `src/App.svelte` | The wallet's one screen |
| `wailsjs/` | The link between the screen and the Go program (the functions in `node/app.go`) |
| `dist/` | The built screens. This is what gets packed into `cvn_node.exe`, and it is kept in the repository so that building the wallet does not need Node.js. Every change and every release is checked automatically: the screens are rebuilt from `src/` and must come out byte for byte as kept here. |

To change the screens, edit `src/`, then run `setup_wallet_deps.bat` in the project folder (it needs [Node.js](https://nodejs.org) 26) followed by `build_clean.bat`, and commit the new `dist/` with the change. By hand: `npm ci --legacy-peer-deps` then `npm run build` in this folder.

The wallet never opens the chain database. Everything it shows comes from a node over HTTP, and transfers are signed on the user's own computer.
