package step

import (
	"context"
	"os/exec"
	"time"
	"tui/config"
)

// KubectlStep polls `kubectl get pods` every 5 s and updates its panel with the result.
// It has no log file — output is sent directly via SetMsg.
type KubectlStep struct{}

func (s *KubectlStep) ID() string                              { return "kubectl" }
func (s *KubectlStep) LogPath(_ string) string                 { return "" }
func (s *KubectlStep) ReadConfig(_ config.InstanceConfig)      {}
func (s *KubectlStep) WriteConfig(_ *config.InstanceConfig)    {}
func (s *KubectlStep) Stop(_ context.Context, _ string) error  { return nil }

// Start begins a background poll loop that runs until ctx is cancelled.
// It polls immediately, then every 5 s.
func (s *KubectlStep) Start(ctx context.Context, _ string) error {
	go func() {
		s.poll()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.poll()
			}
		}
	}()
	return nil
}

func (s *KubectlStep) poll() {
	out, err := exec.Command("kubectl", "get", "pods").CombinedOutput()
	lines := SplitLines(string(out))
	if err != nil && len(lines) == 0 {
		lines = []string{"Waiting for cluster to be ready..."}
	}
	Send(SetMsg{ID: "kubectl", Content: lines})
}
