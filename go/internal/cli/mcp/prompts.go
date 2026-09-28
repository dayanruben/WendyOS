package mcp

import (
	"context"
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerPrompts registers the workflow prompts that walk a client through
// common multi-tool sequences (deploy, diagnose, provision). Handlers are
// pure templating: they never touch the device connection or gRPC.
func (s *mcpServer) registerPrompts(srv *server.MCPServer) {
	srv.AddPrompt(
		mcpgo.NewPrompt("deploy_app",
			mcpgo.WithPromptDescription("Walks through connecting to a device and deploying a project with the run tool."),
			mcpgo.WithArgument("project_path", mcpgo.ArgumentDescription("Path to the project to deploy (defaults to the current directory).")),
			mcpgo.WithArgument("device", mcpgo.ArgumentDescription("Device address from device_list (host:port), vm:NAME or a cloud:// selector; omit to reuse the connection.")),
			mcpgo.WithArgument("device_name", mcpgo.ArgumentDescription("Explicit cloud device name; omit to reuse the current direct, simulator or cloud session target.")),
		),
		s.handleDeployAppPrompt,
	)

	srv.AddPrompt(
		mcpgo.NewPrompt("diagnose_container",
			mcpgo.WithPromptDescription("Walks through diagnosing a container that is failing, crash-looping, or misbehaving."),
			mcpgo.WithArgument("app_name", mcpgo.ArgumentDescription("Name of the app/container to diagnose (defaults to checking all containers).")),
		),
		s.handleDiagnoseContainerPrompt,
	)

	srv.AddPrompt(
		mcpgo.NewPrompt("provision_device",
			mcpgo.WithPromptDescription("Walks through connecting to an unprovisioned device and enrolling it with Wendy Cloud."),
			mcpgo.WithArgument("address", mcpgo.ArgumentDescription("Device address (host:port) to connect to (defaults to discovering one on the LAN).")),
		),
		s.handleProvisionDevicePrompt,
	)
}

// promptArg reads an optional prompt argument, returning defaultVal when the
// argument map is nil or the key is absent/empty.
func promptArg(req mcpgo.GetPromptRequest, name, defaultVal string) string {
	if req.Params.Arguments == nil {
		return defaultVal
	}
	if v, ok := req.Params.Arguments[name]; ok && v != "" {
		return v
	}
	return defaultVal
}

func (s *mcpServer) handleDeployAppPrompt(_ context.Context, req mcpgo.GetPromptRequest) (*mcpgo.GetPromptResult, error) {
	projectPath := promptArg(req, "project_path", ".")
	device := promptArg(req, "device", "")
	argument := ""
	if device != "" {
		argument = fmt.Sprintf(", device=%q", device)
	} else {
		device = promptArg(req, "device_name", "")
		argument = deviceArg(device)
	}

	deviceClause := "the currently connected device"
	if device != "" {
		deviceClause = fmt.Sprintf("device %q", device)
	}

	text := fmt.Sprintf(`Deploy the project at %s to %s.

1. Confirm the intended target with wendy_status. If it is not connected, use device_connect with an address from device_list (host:port), vm:NAME or a cloud:// selector, or cloud_connect for a cloud device name. Reuse a connection only if it is the intended target.
2. Deploy with the run tool: run(project_path=%q%s). This builds the project and starts it on that target. With no connection and no device, run returns NOT_CONNECTED.
3. Check the returned target; if it is not the connected device, device_connect to it first. run always detaches and does not wait for readiness: check container_list for the app's running_state and termination_reason, telemetry_logs for startup errors, and the app's actual health endpoint or output.
`, projectPath, deviceClause, projectPath, argument)

	return mcpgo.NewGetPromptResult(
		"Deploy a project to a Wendy device",
		[]mcpgo.PromptMessage{
			mcpgo.NewPromptMessage(mcpgo.RoleUser, mcpgo.NewTextContent(text)),
		},
	), nil
}

// deviceArg renders the optional device_name argument suffix for the run tool
// call shown in the deploy_app prompt text; this legacy prompt arg is cloud-only.
func deviceArg(device string) string {
	if device == "" {
		return ""
	}
	return fmt.Sprintf(", device_name=%q", device)
}

func (s *mcpServer) handleDiagnoseContainerPrompt(_ context.Context, req mcpgo.GetPromptRequest) (*mcpgo.GetPromptResult, error) {
	appName := promptArg(req, "app_name", "")

	target := "the container"
	listHint := "container_list"
	if appName != "" {
		target = fmt.Sprintf("%q", appName)
		listHint = fmt.Sprintf("container_list (filter for %q)", appName)
	}

	text := fmt.Sprintf(`Diagnose %s.

1. Call %s and inspect running_state and termination_reason. An error_code of ENTITLEMENT_DENIED means the container was denied a capability at start — fix the relevant permission in wendy.json and redeploy.
2. Call container_stats to check CPU/memory usage for signs of resource exhaustion or a crash loop.
3. Call telemetry_logs to read recent stdout/stderr output for the app-level error.
4. If the container is reachable but requests are failing, read the wendy://diagnostics resource — it records container-MCP proxy failures (app name, stage, error, time) that would otherwise only show up on stderr.
`, target, listHint)

	return mcpgo.NewGetPromptResult(
		"Diagnose a failing or misbehaving container",
		[]mcpgo.PromptMessage{
			mcpgo.NewPromptMessage(mcpgo.RoleUser, mcpgo.NewTextContent(text)),
		},
	), nil
}

func (s *mcpServer) handleProvisionDevicePrompt(_ context.Context, req mcpgo.GetPromptRequest) (*mcpgo.GetPromptResult, error) {
	address := promptArg(req, "address", "")

	connectHint := "device_connect (discover the address first with device_list scan=true if you don't have one)"
	if address != "" {
		connectHint = fmt.Sprintf("device_connect(address=%q)", address)
	}

	text := fmt.Sprintf(`Provision a device with Wendy Cloud.

1. Connect to the device: %s.
2. Check provisioning_status to see whether it is already provisioned or awaiting enrollment.
3. If unprovisioned, call provisioning_start with an enrollment_token (and cloud_host/organization_id as needed for your Wendy Cloud account) to begin enrollment.
4. Poll provisioning_status again to watch progress until it reports success (or an error you need to address, e.g. an expired token).
`, connectHint)

	return mcpgo.NewGetPromptResult(
		"Provision a device with Wendy Cloud",
		[]mcpgo.PromptMessage{
			mcpgo.NewPromptMessage(mcpgo.RoleUser, mcpgo.NewTextContent(text)),
		},
	), nil
}
