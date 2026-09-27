package conda

import (
	"encoding/json"
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

func ListEnvs() ([]Env, error) {
	cmd := exec.Command("conda", "env", "list", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result struct {
		Envs []string `json:"envs"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	activeEnv := os.Getenv("CONDA_PREFIX")

	var envs []Env
	for _, path := range result.Envs {
		env := Env{
			Name:   envNameFromPath(path),
			Path:   path,
			Active: path == activeEnv,
		}
		envs = append(envs, env)
	}
	return envs, nil
}

func envNameFromPath(path string) string {
	for _, suffix := range []string{"miniconda3", "anaconda3", "miniforge3", "mambaforge", "miniconda"} {
		if strings.HasSuffix(path, suffix) {
			return "base"
		}
	}
	return filepath.Base(path)
}

func LoadDetails(env *Env) {
	env.PythonVer = getPythonVersion(env.Path)
	env.PkgCount = getPkgCount(env.Path)
	env.SizeBytes = GetDirSize(env.Path)
	env.Loaded = true
}

func getPythonVersion(envPath string) string {
	cmd := exec.Command(pythonBinary(envPath), "--version")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "Python ")
}

func pythonBinary(envPath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(envPath, "python.exe")
	}
	return filepath.Join(envPath, "bin", "python")
}

func getPkgCount(envPath string) int {
	metaDir := filepath.Join(envPath, "conda-meta")
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			count++
		}
	}
	return count
}

func GetDirSize(path string) int64 {
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

func DeleteEnv(env Env) error {
	cmd := exec.Command("conda", "env", "remove", "-n", env.Name, "-y")
	return cmd.Run()
}

func ActivateCmd(env Env) string {
	return "conda activate " + env.Name
}
