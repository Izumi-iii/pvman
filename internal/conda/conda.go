package conda

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	rootPrefix := condaRootPrefix()

	var envs []Env
	for _, path := range result.Envs {
		env := Env{
			Name:   envNameFromPath(path, rootPrefix),
			Path:   path,
			Active: samePath(path, activeEnv),
		}
		envs = append(envs, env)
	}
	return envs, nil
}

// condaRootPrefix returns conda's installation prefix, which is how the base
// environment is identified. Returns "" when `conda info` is unavailable.
func condaRootPrefix() string {
	out, err := exec.Command("conda", "info", "--json").Output()
	if err != nil {
		return ""
	}
	var info struct {
		RootPrefix string `json:"root_prefix"`
	}
	if json.Unmarshal(out, &info) != nil {
		return ""
	}
	return info.RootPrefix
}

func envNameFromPath(path, rootPrefix string) string {
	// The base environment's directory is named after the distribution
	// (`anaconda`, `miniforge`, ...), but conda always refers to it as "base".
	// Ask conda for its root prefix rather than guessing from the path.
	if samePath(path, rootPrefix) {
		return "base"
	}
	// Fallback for when `conda info` failed.
	for _, suffix := range []string{"miniconda3", "anaconda3", "miniforge3", "mambaforge", "miniconda", "anaconda"} {
		if strings.HasSuffix(path, suffix) {
			return "base"
		}
	}
	return filepath.Base(path)
}

// samePath reports whether two paths refer to the same location, tolerating
// trailing separators and Windows case differences.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
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
	cmd := exec.Command("conda", "env", "remove", "-p", env.Path, "-y")
	return cmd.Run()
}

// pkgInfo mirrors the subset of `conda list --json` we care about.
type pkgInfo struct {
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

func listPkgInfo(env Env) ([]pkgInfo, error) {
	// Address the environment by path: it is always unambiguous, whereas the
	// name derived from the directory can be wrong (notably for base).
	cmd := exec.Command("conda", "list", "-p", env.Path, "--json")
	// conda reports failures as JSON on stdout, so capture both streams and
	// surface the first line instead of a bare "exit status 1".
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("%v: %s", err, firstLine(ee.Stderr))
		}
		if s := firstLine(out); s != "" {
			return nil, fmt.Errorf("%v: %s", err, s)
		}
		return nil, err
	}
	var pkgs []pkgInfo
	if err := json.Unmarshal(out, &pkgs); err != nil {
		return nil, err
	}
	return pkgs, nil
}

// ListPackages returns the sorted, de-duplicated package names in an environment.
// conda can report the same name more than once when it was installed from
// several channels, so duplicates are collapsed.
func ListPackages(env Env) ([]string, error) {
	pkgs, err := listPkgInfo(env)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(pkgs))
	names := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		if p.Name == "" || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names, nil
}

// RemovePackage removes packages from a conda environment.
//
// Packages installed from the "pypi" channel were put there by pip, and
// `conda remove` cannot touch them, so they are uninstalled with the
// environment's own pip instead. Everything else goes through conda.
func RemovePackage(env Env, pkgs ...string) error {
	if len(pkgs) == 0 {
		return nil
	}

	channelOf := make(map[string]string, len(pkgs))
	if info, err := listPkgInfo(env); err == nil {
		for _, p := range info {
			// Prefer a conda channel when the same name shows up twice.
			if prev, ok := channelOf[p.Name]; !ok || prev == "pypi" {
				channelOf[p.Name] = p.Channel
			}
		}
	}

	var condaPkgs, pipPkgs []string
	for _, name := range pkgs {
		if channelOf[name] == "pypi" {
			pipPkgs = append(pipPkgs, name)
		} else {
			condaPkgs = append(condaPkgs, name)
		}
	}

	if len(condaPkgs) > 0 {
		args := append([]string{"remove", "-p", env.Path, "-y"}, condaPkgs...)
		if out, err := exec.Command("conda", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, firstLine(out))
		}
	}

	if len(pipPkgs) > 0 {
		// -y so pip never blocks waiting for a confirmation prompt.
		args := append([]string{"-m", "pip", "uninstall", "-y"}, pipPkgs...)
		if out, err := exec.Command(pythonBinary(env.Path), args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, firstLine(out))
		}
	}

	return nil
}

// firstLine keeps CLI error output to one readable line for the status bar.
func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func ActivateCmd(env Env) string {
	return "conda activate " + env.Name
}
