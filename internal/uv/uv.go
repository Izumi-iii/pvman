package uv

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Env struct {
	Name      string
	Path      string
	Active    bool
	PythonVer string
	PkgCount  int
	SizeBytes int64
	Loaded    bool
}

// ScanDir finds all uv venvs in the given directory (one level deep).
func ScanDir(dir string) ([]Env, error) {
	activeEnv, _ := filepath.Abs(os.Getenv("VIRTUAL_ENV"))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var envs []Env
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		envPath := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(envPath, "pyvenv.cfg")); err == nil {
			absPath, _ := filepath.Abs(envPath)
			env := Env{
				Name:   e.Name(),
				Path:   envPath,
				Active: absPath == activeEnv,
			}
			envs = append(envs, env)
		}
	}
	return envs, nil
}

func LoadDetails(env *Env) {
	env.PythonVer = getPythonVersionFromCfg(env.Path)
	env.PkgCount = getPkgCount(env.Path)
	env.SizeBytes = getDirSize(env.Path)
	env.Loaded = true
}

func getPythonVersionFromCfg(envPath string) string {
	f, err := os.Open(filepath.Join(envPath, "pyvenv.cfg"))
	if err != nil {
		return "unknown"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "version") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func getPkgCount(envPath string) int {
	cmd := exec.Command("uv", "pip", "list", "--python", pythonBinary(envPath))
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= 2 {
		return 0
	}
	return len(lines) - 2
}

func pythonBinary(envPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(envPath, "Scripts", "python.exe")
	}
	return filepath.Join(envPath, "bin", "python")
}

func getDirSize(path string) int64 {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size += info.Size()
		return nil
	})
	return size
}

func CreateEnv(dir, name, pythonVer string) error {
	args := []string{"venv", filepath.Join(dir, name)}
	if pythonVer != "" {
		args = append(args, "--python", pythonVer)
	}
	cmd := exec.Command("uv", args...)
	return cmd.Run()
}

func DeleteEnv(env Env) error {
	return os.RemoveAll(env.Path)
}

func ActivateCmd(env Env) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(env.Path, "Scripts", "activate.bat")
	}
	return "source " + fmt.Sprintf("%q", filepath.Join(env.Path, "bin", "activate"))
}
