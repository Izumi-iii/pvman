# pvman

<table>
	<tr>
		<td width="180" align="center">
			<img src="https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/b842f40fe85f3b70cc2f7b89a9f6e7e0.png" alt="pvman icon" width="160">
		</td>
		<td>
			<h2 align="center">
				<span style="font-family: 'Hiragino Maru Gothic ProN', 'Yu Gothic', 'Comic Sans MS', cursive; font-size: 1.4em; font-style: italic; font-weight: 900; letter-spacing: 0.08em; padding: 0 10px 4px; border-bottom: 3px solid #9bdcff; text-shadow: 1px 1px 0 #d9f3ff;">
					<span style="color: #4db8ff;">pv</span><span style="color: #ff4d5a;">man</span>
				</span>
			</h2>
			A terminal UI for managing Python virtual environments, with conda and uv side by side.
		</td>
	</tr>
</table>

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

![1790529914591.png](https://tk-pichost-1325224430.cos.ap-chengdu.myqcloud.com/blog/1790529914591.png)

## Install

### Homebrew (macOS / Linux)

```bash
brew install <your-tap>/pvman
```

### Go install

```bash
go install github.com/tkzzzzzz6/pvman@latest
```

### Download binary

Grab the latest binary for your platform from the [Releases](https://github.com/tkzzzzzz6/pvman/releases) page.

| Platform | File |
|----------|------|
| macOS Apple Silicon | `pvman-darwin-arm64` |
| Linux x86_64 | `pvman-linux-amd64` |
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
git clone https://github.com/tkzzzzzz6/pvman.git
cd pvman
go build -ldflags="-s -w" -o pvman .
```

## License

[MIT License](LICENSE)
