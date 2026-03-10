package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// generateSkaffoldYAML writes a skaffold.yaml to a temp file with the given
// localPort for port-forwarding and returns its path along with the active
// profile slice derived from env.
func generateSkaffoldYAML(dir, env, localPort string) (path string, profiles []string, err error) {
	content := fmt.Sprintf(`apiVersion: skaffold/v4beta11
kind: Config
metadata:
  name: hello-world

build:
  artifacts:
    - image: hello-world
      docker:
        dockerfile: Dockerfile
      context: .

manifests:
  rawYaml:
    - k8s/deployment.yaml
    - k8s/service.yaml

portForward:
  - resourceType: Service
    resourceName: hello-world
    port: 8080
    localPort: %s

profiles:
  - name: dev
    # Uses the base manifests — APP_ENV=dev is set in deployment.yaml
  - name: test
    manifests:
      rawYaml:
        - k8s/deployment-test.yaml
        - k8s/service.yaml
`, localPort)

	f, err := os.CreateTemp("", "skaffold-*.yaml")
	if err != nil {
		return "", nil, fmt.Errorf("create temp skaffold.yaml: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("write temp skaffold.yaml: %w", err)
	}
	f.Close()

	// Copy Dockerfile and k8s dir references are relative to dir in skaffold;
	// skaffold resolves paths relative to the skaffold.yaml location, but the
	// artifacts context must match. We write a symlink-free approach: pass
	// --filename pointing to our temp file, but skaffold uses its directory as
	// the context root. To avoid this, write the file into the sample dir itself
	// so relative paths (Dockerfile, k8s/) resolve correctly.
	dest := filepath.Join(dir, ".skaffold-gen.yaml")
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		os.Remove(f.Name())
		return "", nil, fmt.Errorf("write skaffold-gen.yaml: %w", err)
	}
	os.Remove(f.Name())

	if env != "" {
		profiles = []string{env}
	}
	return dest, profiles, nil
}
