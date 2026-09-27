package hosting

import (
	"os"
	"strings"
)

// InContainer reports whether this process is running inside one.
//
// Two signals, because neither is universal: the file Docker leaves behind, and
// the container runtime in this process's own cgroup - the second catches podman
// and a plain containerd, the first catches a Docker container whose cgroup has
// been namespaced away.
func InContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}

	cgroup, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return false
	}

	text := string(cgroup)

	return strings.Contains(text, "docker") || strings.Contains(text, "containerd") ||
		strings.Contains(text, "kubepods")
}
