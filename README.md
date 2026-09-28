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

### One-line install (Linux / macOS)

The install script automatically detects the operating system and CPU architecture,
installs Go 1.26.1 to the current user's home directory, configures `PATH`, and
installs `pvman`:

```bash
curl -fsSL --connect-timeout 15 --max-time 60 https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | sh
```

Or use `wget`:

```bash
wget --timeout=15 --tries=1 -qO- https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | sh
```

The script supports Linux and macOS on `amd64` and `arm64`. After installation,
reload your shell and run the program:

```bash
source ~/.bashrc  # use ~/.zshrc for zsh
pvman
```

To install another Go version, set `PV_MAN_GO_VERSION` before running the script:

```bash
curl -fsSL --connect-timeout 15 --max-time 60 https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh | PV_MAN_GO_VERSION=1.26.1 sh
```

### One-line install (Windows)

Run the following command in PowerShell. It installs Go 1.26.1 and `pvman` for
the current Windows user, without requiring administrator privileges:

```powershell
irm https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.ps1 | iex
```

Alternatively, download and run the batch file from Command Prompt or PowerShell:

```bat
curl.exe -fsSL https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.bat -o install-pvman.bat
install-pvman.bat
```

The Windows installer supports `amd64` and `arm64`. Open a new terminal after
installation, then run `pvman`.

### WSL network troubleshooting

If `wget` appears to hang or `curl` reports `Proxy CONNECT aborted`, the problem
is usually the WSL proxy configuration, not `pvman`. `ping github.com` only tests
ICMP and does not verify HTTPS or proxy connectivity.

Check the proxy settings currently used by WSL:

```bash
env | grep -iE '^(http|https|all|no)_proxy='
git config --global --get-regexp 'http.*proxy|https.*proxy' || true
```

If no proxy is required, temporarily clear the proxy variables and test HTTPS:

```bash
unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy
curl -I --connect-timeout 10 --max-time 20 https://raw.githubusercontent.com/tkzzzzzz6/pvman/main/scripts/install.sh
```

If a proxy is required, configure a reachable WSL proxy address instead. The
installer needs HTTPS access to `raw.githubusercontent.com`, `go.dev`, and Go's
module proxy. After fixing the network, rerun the installation command.

### Install a specific Go version on Linux

The project requires Go 1.26.1 or newer. Set `GO_VERSION` to the version you want;
the following commands install Go 1.26.1 on 64-bit x86 Linux:

```bash
GO_VERSION=1.26.1
curl -LO "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go${GO_VERSION}.linux-amd64.tar.gz"
echo 'export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
go version
```

For ARM64 Linux, replace `linux-amd64` with `linux-arm64` in the download URL and
use the matching archive name in the `tar` command.

On Ubuntu or Debian, `apt` can install Go quickly, but it does not let you reliably
select the exact Go version:

```bash
sudo apt update
sudo apt install -y golang-go
go version
```

If the package manager installs a version older than Go 1.26.1, use the official
installation commands above instead.

### Fix `invalid go version` when building

If `go version` shows an old release such as Go 1.19.8 and `go build` reports:

```text
go: errors parsing go.mod:
invalid go version '1.26.1': must match format 1.23
```

the active Go toolchain is too old for this project. The `go.mod` file requires
Go 1.26.1, so installing or selecting Go 1.19 is not sufficient. After installing
a newer Go version, refresh the shell and verify which executable is being used:

```bash
source ~/.bashrc
hash -r
which go
go version
go env GOROOT
```

`which go` should point to `/usr/local/go/bin/go`, and `go version` should report
Go 1.26.1 or newer. If Conda still provides the old executable, temporarily leave
the base environment and reload the shell before building:

```bash
conda deactivate
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
go version
go build -ldflags="-s -w" -o pvman .
```

Do not fix this error by changing `go.mod` to `go 1.19`; that only hides the
version mismatch and may cause newer dependencies or language features to fail.

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
