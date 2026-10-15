// Command validate-registry is a local-only helper. It checks the registry
// entry against the CPA plugin store validator and verifies a dist/ directory
// the same way CPA's installer would (archive name, single root library,
// sha256 from checksums.txt).
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginstore"
)

var extByOS = map[string]string{"darwin": ".dylib", "linux": ".so", "windows": ".dll"}

func main() {
	dist := flag.String("dist", "", "dist directory with zips and checksums.txt")
	entry := flag.String("entry", "", "registry entry json")
	flag.Parse()
	failed := false
	fail := func(format string, a ...any) {
		failed = true
		fmt.Printf("FAIL  "+format+"\n", a...)
	}

	var plugin pluginstore.Plugin
	if *entry != "" {
		data, err := os.ReadFile(*entry)
		if err != nil {
			fail("read entry: %v", err)
		} else if err := json.Unmarshal(data, &plugin); err != nil {
			fail("parse entry: %v", err)
		} else if err := pluginstore.ValidatePlugin(plugin); err != nil {
			fail("ValidatePlugin: %v", err)
		} else {
			fmt.Printf("OK    registry entry %s %s (install=%s)\n", plugin.ID, plugin.Version, pluginstore.PluginInstallType(plugin))
		}
	}

	if *dist != "" {
		checkDist(*dist, plugin, fail)
	}
	if failed {
		fmt.Println("RESULT: FAIL")
		os.Exit(1)
	}
	fmt.Println("RESULT: PASS")
}

func checkDist(dir string, plugin pluginstore.Plugin, fail func(string, ...any)) {
	id := plugin.ID
	if id == "" {
		id = "live-routes"
	}
	sums, err := parseChecksums(filepath.Join(dir, "checksums.txt"))
	if err != nil {
		fail("checksums.txt: %v", err)
		return
	}
	zips, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	if len(zips) == 0 {
		fail("no zip files in %s", dir)
		return
	}
	for _, z := range zips {
		name := filepath.Base(z)
		// <id>_<version>_<goos>_<goarch>.zip
		parts := strings.Split(strings.TrimSuffix(name, ".zip"), "_")
		if len(parts) != 4 || parts[0] != id {
			fail("%s: name does not match <id>_<version>_<goos>_<goarch>.zip", name)
			continue
		}
		version, goos, goarch := parts[1], parts[2], parts[3]
		if want := archiveName(id, version, goos, goarch); want != name {
			fail("%s: expected %s", name, want)
			continue
		}
		if plugin.Version != "" && plugin.Version != version {
			fail("%s: version %s differs from registry entry %s", name, version, plugin.Version)
		}
		ext, ok := extByOS[goos]
		if !ok {
			fail("%s: unknown goos %s", name, goos)
			continue
		}
		data, err := os.ReadFile(z)
		if err != nil {
			fail("%s: %v", name, err)
			continue
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if want := sums[name]; want == "" {
			fail("%s: missing from checksums.txt", name)
			continue
		} else if want != got {
			fail("%s: sha256 mismatch (checksums.txt %s, actual %s)", name, want, got)
			continue
		}
		if err := checkZip(z, id, version, ext); err != nil {
			fail("%s: %v", name, err)
			continue
		}
		fmt.Printf("OK    %s  sha256=%s\n", name, got[:12])
	}
}

func archiveName(id, version, goos, goarch string) string {
	return fmt.Sprintf("%s_%s_%s_%s.zip", id, version, goos, goarch)
}

func checkZip(file, id, version, ext string) error {
	r, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer r.Close()
	target, versioned := id+ext, id+"-v"+version+ext
	count := 0
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		clean := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		if strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return fmt.Errorf("unsafe entry %q", f.Name)
		}
		if !f.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("entry %q is not a regular file", f.Name)
		}
		e := path.Ext(clean)
		if e != ".dylib" && e != ".so" && e != ".dll" {
			continue
		}
		if clean != target && clean != versioned {
			if path.Base(clean) == target || path.Base(clean) == versioned {
				return fmt.Errorf("library must be at zip root, found %q", f.Name)
			}
			return fmt.Errorf("library must be named %s or %s, found %q", target, versioned, f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		n, _ := io.Copy(io.Discard, rc)
		rc.Close()
		if n == 0 {
			return fmt.Errorf("library %q is empty", f.Name)
		}
		count++
	}
	if count != 1 {
		return fmt.Errorf("expected exactly one dynamic library, found %d", count)
	}
	return nil
}

func parseChecksums(file string) (map[string]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 || len(f[0]) != 64 {
			return nil, fmt.Errorf("line %d: invalid entry", i+1)
		}
		out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
	}
	return out, nil
}
