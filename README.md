# pvman

A terminal UI for managing Python virtual environments — conda and uv, side by side.

![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)
![License](https://img.shields.io/badge/license-MIT-green)

## Features

- View all **conda** environments with Python version, package count, and size
- Scan current directory for **uv** virtual environments (`.venv` and named envs)
- Async detail loading — size and package info loads in the background
- Create new uv venvs with a specific Python version
- Delete conda or uv environments with confirmation
- One-click copy of the activation command
- Vim-style (`j`/`k`) and arrow key navigation

## Demo

```
╭─ Environments ──────────────╮ ╭─ Details ─────────────────────────────╮
│  conda                      │ │ agent          conda                   │
│    base              3.13   │ │                                        │
│  ► agent             3.11   │ │ Python    3.11.9                       │
│                             │ │ Packages  42                           │
│  uv  (~/code/myproject)     │ │ Size      1.2 GB                       │
│    .venv             3.12   │ │ Path      ~/miniconda3/envs/agent      │
│                             │ │                                        │
│                             │ │ Activate                               │
│                             │ │ conda activate agent                   │
╰─────────────────────────────╯ ╰────────────────────────────────────────╯
 pvman  n new  d delete  r refresh  q quit
```

## Install

### Homebrew (macOS / Linux)

```bash
brew install <your-tap>/pvman
```

### Go install

```bash
go install github.com/yourusername/pvman@latest
```

### Download binary

Grab the latest binary for your platform from the [Releases](https://github.com/yourusername/pvman/releases) page.

| Platform | File |
|----------|------|
| macOS Apple Silicon | `pvman-darwin-arm64` |
| macOS Intel | `pvman-darwin-amd64` |
| Linux x86_64 | `pvman-linux-amd64` |
| Linux ARM64 | `pvman-linux-arm64` |
| Windows x86_64 | `pvman-windows-amd64.exe` |

## Usage

Run `pvman` from any project directory:

```bash
pvman
```

It will show all your conda environments and scan the current directory for uv venvs.

## Key Bindings

| Key | Action |
|-----|--------|
| `j` / `↓` | Move down |
| `k` / `↑` | Move up |
| `n` | Create new uv environment |
| `d` | Delete selected environment |
| `r` | Refresh list |
| `q` | Quit |
| `esc` | Cancel / close dialog |

## Requirements

- [conda](https://docs.conda.io/) / [miniconda](https://docs.anaconda.com/miniconda/) for conda env support
- [uv](https://docs.astral.sh/uv/) for uv env support

## Build from source

```bash
git clone https://github.com/yourusername/pvman.git
cd pvman
go build -ldflags="-s -w" -o pvman .
```

## License

MIT
