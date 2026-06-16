package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wendylabsinc/wendy/go/internal/shared/appconfig"
	"github.com/wendylabsinc/wendy/go/internal/shared/models"
)

var (
	appleContainerCommandContext = exec.CommandContext
	appleContainerLookPath       = exec.LookPath
	appleContainerHostGOOS       = func() string { return runtime.GOOS }
	appleContainerHostGOARCH     = func() string { return runtime.GOARCH }
)

type appleContainerBuildContext struct {
	ImageName     string
	ContainerName string
	cmd           *exec.Cmd
}

// AppleContainerProvider builds and runs Dockerfile/Containerfile projects
// with Apple's container CLI on Apple silicon Macs.
type AppleContainerProvider struct{}

func (p *AppleContainerProvider) Key() string         { return ProviderKeyAppleContainer }
func (p *AppleContainerProvider) DisplayName() string { return "Apple Container" }

func (p *AppleContainerProvider) IsAvailable(ctx context.Context) bool {
	if !appleContainerSupportedHost(appleContainerHostGOOS(), appleContainerHostGOARCH()) {
		return false
	}
	if _, err := appleContainerLookPath("container"); err != nil {
		return false
	}
	return appleContainerCommandContext(ctx, "container", "--version").Run() == nil
}

func appleContainerSupportedHost(goos, goarch string) bool {
	return goos == "darwin" && goarch == "arm64"
}

func (p *AppleContainerProvider) CheckRequirements(ctx context.Context) error {
	if !appleContainerSupportedHost(appleContainerHostGOOS(), appleContainerHostGOARCH()) {
		return fmt.Errorf("Apple Container requires an Apple silicon Mac")
	}
	if _, err := appleContainerLookPath("container"); err != nil {
		return fmt.Errorf("container CLI is not installed or not in PATH")
	}
	if err := appleContainerCommandContext(ctx, "container", "--version").Run(); err != nil {
		return fmt.Errorf("container CLI is not usable: %w", err)
	}
	cmd := appleContainerCommandContext(ctx, "container", "system", "status")
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			msg = ": " + msg
		}
		return fmt.Errorf("Apple Container system is not running%s. Run 'container system start' and try again: %w", msg, err)
	}
	return nil
}

func (p *AppleContainerProvider) DiscoverDevices(ctx context.Context) ([]models.ExternalDevice, error) {
	if !p.IsAvailable(ctx) {
		return nil, nil
	}
	version := appleContainerVersion(ctx)
	return []models.ExternalDevice{
		{
			ID:              p.Key(),
			DisplayName:     p.DisplayName(),
			ProviderKey:     p.Key(),
			IsWendyDevice:   false,
			AgentVersion:    version,
			OS:              "linux",
			CPUArchitecture: "arm64",
		},
	}, nil
}

func appleContainerVersion(ctx context.Context) string {
	out, err := appleContainerCommandContext(ctx, "container", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (p *AppleContainerProvider) SupportedBuildTypes() []string {
	return []string{"docker"}
}

func (p *AppleContainerProvider) CanBuild(projectPath string) bool {
	return hasContainerBuildFile(projectPath)
}

func (p *AppleContainerProvider) Build(ctx context.Context, device models.ExternalDevice, projectPath, product string, debug bool) (*BuiltApp, error) {
	return p.BuildWithDockerfile(ctx, device, projectPath, product, "", "", debug)
}

func (p *AppleContainerProvider) BuildWithType(ctx context.Context, device models.ExternalDevice, projectPath, product, buildType string, debug bool) (*BuiltApp, error) {
	return p.BuildWithDockerfile(ctx, device, projectPath, product, buildType, "", debug)
}

func (p *AppleContainerProvider) BuildWithDockerfile(ctx context.Context, device models.ExternalDevice, projectPath, product, buildType, dockerfile string, debug bool) (*BuiltApp, error) {
	switch strings.TrimSpace(strings.ToLower(buildType)) {
	case "", "docker":
	default:
		return nil, fmt.Errorf("Apple Container supports Dockerfile/Containerfile builds only, not %s", buildType)
	}
	if err := p.CheckRequirements(ctx); err != nil {
		return nil, err
	}

	buildContext, err := appleContainerBuildContextPath(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolving project path: %w", err)
	}

	imageName := strings.ToLower(product) + ":latest"
	platform := "linux/arm64"
	if device.CPUArchitecture != "" {
		platform = "linux/" + device.CPUArchitecture
	}
	args := []string{"build", "--platform", platform, "-t", imageName}
	if dockerfile == "" {
		dockerfile = defaultContainerBuildFile(projectPath)
	}
	if dockerfile != "" {
		resolved, err := confinedProviderDockerfilePath(projectPath, dockerfile)
		if err != nil {
			return nil, err
		}
		args = append(args, "-f", resolved)
	}
	args = append(args, buildContext)

	cmd := appleContainerCommandContext(ctx, "container", args...)
	cmd.Dir = buildContext
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("container build: %w", err)
	}
	return &BuiltApp{
		ProviderKey: p.Key(),
		Device:      device,
		AppName:     product,
		Context: &appleContainerBuildContext{
			ImageName:     imageName,
			ContainerName: product,
		},
	}, nil
}

func appleContainerBuildContextPath(projectPath string) (string, error) {
	buildContext, err := filepath.Abs(projectPath)
	if err != nil {
		return "", err
	}
	// Apple Container 1.0.0 mishandles symlink-resolved /private/tmp build
	// contexts. When macOS reports /private/tmp, pass the equivalent /tmp path.
	if appleContainerHostGOOS() == "darwin" {
		if normalized, ok := appleContainerTmpAlias(buildContext); ok {
			return normalized, nil
		}
	}
	return buildContext, nil
}

func appleContainerTmpAlias(path string) (string, bool) {
	const privateTmp = "/private/tmp"
	if path != privateTmp && !strings.HasPrefix(path, privateTmp+"/") {
		return "", false
	}
	candidate := "/tmp" + strings.TrimPrefix(path, privateTmp)
	if sameFilePath(path, candidate) {
		return candidate, true
	}
	return "", false
}

func sameFilePath(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	if leftErr != nil {
		return false
	}
	rightInfo, rightErr := os.Stat(right)
	if rightErr != nil {
		return false
	}
	return os.SameFile(leftInfo, rightInfo)
}

func confinedProviderDockerfilePath(projectPath, dockerfile string) (string, error) {
	absProject, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return "", fmt.Errorf("resolving project path: %w", err)
	}
	joined, err := filepath.Abs(filepath.Join(absProject, dockerfile))
	if err != nil {
		return "", fmt.Errorf("resolving dockerfile path: %w", err)
	}
	escapes := func(rel string) bool {
		return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	rel, err := filepath.Rel(absProject, joined)
	if err != nil || escapes(rel) {
		return "", fmt.Errorf("dockerfile %q must be within the project directory", dockerfile)
	}
	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("dockerfile %q: %w", dockerfile, err)
	}
	rel, err = filepath.Rel(absProject, resolved)
	if err != nil || escapes(rel) {
		return "", fmt.Errorf("dockerfile %q must be within the project directory", dockerfile)
	}
	return resolved, nil
}

func (p *AppleContainerProvider) Run(ctx context.Context, app *BuiltApp, detach bool, output chan<- RunOutput) error {
	defer close(output)

	bc, ok := app.Context.(*appleContainerBuildContext)
	if !ok {
		return fmt.Errorf("Apple Container provider: invalid build context")
	}
	if err := p.CheckRequirements(ctx); err != nil {
		return err
	}
	if err := p.removeManagedContainer(ctx, bc.ContainerName); err != nil {
		return err
	}

	args := []string{"run", "--name", bc.ContainerName, "--label", "wendy.managed=true"}
	for k, v := range appconfig.BuildEntitlementAnnotations(app.Entitlements) {
		args = append(args, "--label", k+"="+v)
	}
	if detach {
		args = append(args, "--detach")
	}
	args = append(args, bc.ImageName)

	cmd := appleContainerCommandContext(ctx, "container", args...)
	bc.cmd = cmd

	if detach {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("container run: %w", err)
		}
		output <- RunOutput{Type: RunOutputStarted}
		return nil
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("container run: %w", err)
	}

	output <- RunOutput{Type: RunOutputStarted}

	done := make(chan struct{})
	go scanLines(stdoutPipe, output, RunOutputStdout, done)
	go scanLines(stderrPipe, output, RunOutputStderr, done)

	<-done
	<-done
	return cmd.Wait()
}

func (p *AppleContainerProvider) Stop(ctx context.Context, app *BuiltApp) error {
	bc, ok := app.Context.(*appleContainerBuildContext)
	if !ok {
		return fmt.Errorf("Apple Container provider: invalid build context")
	}
	cmd := appleContainerCommandContext(ctx, "container", "stop", bc.ContainerName)
	return cmd.Run()
}

func (p *AppleContainerProvider) removeManagedContainer(ctx context.Context, name string) error {
	managed, err := p.containerHasManagedLabel(ctx, name)
	if err != nil || !managed {
		return nil
	}
	cmd := appleContainerCommandContext(ctx, "container", "delete", "--force", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("container delete: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (p *AppleContainerProvider) containerHasManagedLabel(ctx context.Context, name string) (bool, error) {
	cmd := appleContainerCommandContext(ctx, "container", "inspect", name)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return appleContainerInspectHasManagedLabel(out), nil
}

func appleContainerInspectHasManagedLabel(data []byte) bool {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		s := string(data)
		return strings.Contains(s, "wendy.managed") && strings.Contains(s, "true")
	}
	return jsonContainsManagedLabel(v)
}

func jsonContainsManagedLabel(v any) bool {
	switch value := v.(type) {
	case []any:
		for _, item := range value {
			if jsonContainsManagedLabel(item) {
				return true
			}
		}
	case map[string]any:
		for k, item := range value {
			if k == "wendy.managed" && fmt.Sprint(item) == "true" {
				return true
			}
			if jsonContainsManagedLabel(item) {
				return true
			}
		}
	}
	return false
}

func (p *AppleContainerProvider) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	cmd := appleContainerCommandContext(ctx, "container", "list", "--all", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("container list: %w", err)
	}

	var containers []ContainerInfo
	for _, info := range appleContainerListInfos(out) {
		if info.Name == "" {
			continue
		}
		managed, _ := p.containerHasManagedLabel(ctx, info.Name)
		if managed {
			containers = append(containers, info)
		}
	}
	return containers, nil
}

func appleContainerListInfos(data []byte) []ContainerInfo {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	var entries []any
	switch value := v.(type) {
	case []any:
		entries = value
	case map[string]any:
		if items, ok := value["items"].([]any); ok {
			entries = items
		}
	}

	containers := make([]ContainerInfo, 0, len(entries))
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		image := firstJSONText(m, "image", "Image")
		if image == "" {
			if cfg := firstJSONMap(m, "configuration", "Configuration"); cfg != nil {
				if img := firstJSONMap(cfg, "image", "Image"); img != nil {
					image = firstJSONText(img, "reference", "Reference")
				}
			}
		}
		state := firstJSONText(m, "state", "State")
		status := firstJSONText(m, "status", "Status")
		if statusMap := firstJSONMap(m, "status", "Status"); statusMap != nil {
			if nestedState := firstJSONText(statusMap, "state", "State"); nestedState != "" {
				if state == "" {
					state = nestedState
				}
				if status == "" {
					status = nestedState
				}
			}
		}
		containers = append(containers, ContainerInfo{
			Name:   firstJSONText(m, "id", "ID", "name", "Name"),
			Image:  image,
			State:  state,
			Status: status,
		})
	}
	return containers
}

func firstJSONText(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case string:
				return v
			case json.Number:
				return v.String()
			case float64, bool:
				return fmt.Sprint(v)
			}
		}
	}
	return ""
}

func firstJSONMap(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := m[key].(map[string]any); ok {
			return value
		}
	}
	return nil
}

func (p *AppleContainerProvider) StartContainer(ctx context.Context, name string) error {
	cmd := appleContainerCommandContext(ctx, "container", "start", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("container start: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (p *AppleContainerProvider) StopContainer(ctx context.Context, name string) error {
	cmd := appleContainerCommandContext(ctx, "container", "stop", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("container stop: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (p *AppleContainerProvider) RemoveContainer(ctx context.Context, name string) error {
	cmd := appleContainerCommandContext(ctx, "container", "delete", "--force", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("container delete: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
