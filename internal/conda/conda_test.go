package conda

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeMeta lays down a fake conda-meta record.
func writeMeta(t *testing.T, dir, file, name string, depends []string) {
	t.Helper()
	body := `{"name":"` + name + `","depends":[`
	for i, d := range depends {
		if i > 0 {
			body += ","
		}
		body += `"` + d + `"`
	}
	body += `]}`
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDependencies(t *testing.T) {
	meta := t.TempDir()
	writeMeta(t, meta, "requests-2.32.2-0.json", "requests",
		[]string{"certifi >=2017.4.17", "charset-normalizer >=2,<4", "urllib3 >=1.21.1,<3", "__win"})
	writeMeta(t, meta, "certifi-2024.8.30-0.json", "certifi", nil)
	writeMeta(t, meta, "charset-normalizer-3.3.2-0.json", "charset-normalizer", nil)
	writeMeta(t, meta, "urllib3-2.2.3-0.json", "urllib3", nil)
	// Depends on something that is not installed.
	writeMeta(t, meta, "ghost-1.0-0.json", "ghost", []string{"absent-package >=1.0"})

	e := Env{Name: "test", Path: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(e.Path, "conda-meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(meta)
	for _, en := range entries {
		raw, _ := os.ReadFile(filepath.Join(meta, en.Name()))
		if err := os.WriteFile(filepath.Join(e.Path, "conda-meta", en.Name()), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	deps, dependents, err := Dependencies(e)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"certifi", "charset-normalizer", "urllib3"}
	if got := deps["requests"]; !reflect.DeepEqual(got, want) {
		t.Errorf("requests depends on %v, want %v (__win and absent packages must be dropped)", got, want)
	}

	if got := dependents["certifi"]; !reflect.DeepEqual(got, []string{"requests"}) {
		t.Errorf("certifi required by %v, want [requests]", got)
	}

	if got := deps["ghost"]; len(got) != 0 {
		t.Errorf("ghost depends on %v, want none (target is not installed)", got)
	}

	if _, ok := dependents["absent-package"]; ok {
		t.Error("an edge to an uninstalled package must not appear in the reverse index")
	}
}

func TestDepName(t *testing.T) {
	cases := map[string]string{
		"certifi >=2017.4.17":       "certifi",
		"python >=3.12,<3.13.0a0":   "python",
		"libgcc-ng >=12":            "libgcc-ng",
		"charset-normalizer >=2,<4": "charset-normalizer",
		"pip":                       "pip",
		"  numpy 1.24.*  ":          "numpy",
		"python_abi 3.10.* *_cp310": "python_abi",
		"":                          "",
	}
	for in, want := range cases {
		if got := depName(in); got != want {
			t.Errorf("depName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDependenciesOnMissingEnv(t *testing.T) {
	if _, _, err := Dependencies(Env{Name: "gone", Path: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("expected an error for an environment with no conda-meta directory")
	}
}
