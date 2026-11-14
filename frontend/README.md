# Desktop wallet screens

This folder holds the screens of the Windows desktop wallet (`cvn_node.exe`). They are written with [Svelte](https://svelte.dev) and built with [Vite](https://vite.dev).

| Path | What it is |
|---|---|
| `src/App.svelte` | The wallet's one screen |
| `wailsjs/` | The link between the screen and the Go program (the functions in `app.go`) |
| `dist/` | The built screens. This is what gets packed into `cvn_node.exe`, and it is kept in the repository so that building the wallet does not need Node.js. |

To change the screens, edit `src/`, then run `setup_wallet_deps.bat` in the project folder (it needs [Node.js](https://nodejs.org)) followed by `build_clean.bat`.

The wallet never opens the chain database. Everything it shows comes from a node over HTTP, and transfers are signed on the user's own computer.
