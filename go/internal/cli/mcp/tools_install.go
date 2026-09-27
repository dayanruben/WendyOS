package mcp

import (
	"context"
	"errors"
	"os"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/wendylabsinc/wendy/go/internal/cli/onboarding"
)

// SetInstallationBackend must be called before Start.
func (s *mcpServer) SetInstallationBackend(backend onboarding.Backend) { s.installation = backend }

func (s *mcpServer) registerInstallationTools(srv *server.MCPServer) {
	plan := []mcpgo.ToolOption{
		mcpgo.WithDescription("Plan initial WendyOS or agent installation from this host, without a device connection or disk writes. Resolves published artifacts, erase scope, target checks and CLI argument array. Supports Raspberry Pi, Jetson developer kits, Unitree G1 PC2 and Linux agent installs. Run the returned command in a terminal for elevation and flash progress; use os_install_verify after boot."),
		mcpgo.WithString("device_type", mcpgo.Required(), mcpgo.Description("Exact board: raspberry-pi-3/4/5, jetson-orin-nano, jetson-agx-orin, jetson-agx-thor, unitree-g1, or linux-desktop")),
		mcpgo.WithString("carrier", mcpgo.Description("Required for Jetson: developer-kit. Custom robot carriers need vendor instructions, not generic Jetson images.")),
		mcpgo.WithString("version", mcpgo.Description("Exact WendyOS version; omitted resolves latest stable")),
		mcpgo.WithString("storage", mcpgo.Enum("sd", "nvme", "emmc")),
		mcpgo.WithString("drive", mcpgo.Description("Exact host disk path from os_list_drives; only for raw-media writes")),
		mcpgo.WithBoolean("rootfs_only", mcpgo.Description("Orin raw-media write only; leaves QSPI firmware unchanged")),
	}
	plan = append(plan, readOnly()...)
	plan = append(plan, openWorld()...)
	srv.AddTool(mcpgo.NewTool("os_install_plan", plan...), s.handleInstallPlan)
	drives := []mcpgo.ToolOption{mcpgo.WithDescription("List potential installation drives attached to this host, including non-removable drives, excluding the system drive. No active device connection needed. Match path, model and capacity before planning a write; removable status is not authorization to erase.")}
	drives = append(drives, readOnly()...)
	drives = append(drives, localOnly()...)
	srv.AddTool(mcpgo.NewTool("os_list_drives", drives...), s.handleInstallDrives)
	verify := []mcpgo.ToolOption{
		mcpgo.WithDescription("Verify first boot at an explicit target without changing the active MCP connection. Checks the agent, optional expected OS/board/public key and enrollment. Repeat during boot if unreachable. Application behavior is reported as not_checked."),
		mcpgo.WithString("address", mcpgo.Required(), mcpgo.Description("Explicit hostname, IP:port, or cloud selector; never chooses a default device")),
		mcpgo.WithString("expected_os_version"), mcpgo.WithString("expected_device_type"), mcpgo.WithString("expected_public_key"),
		mcpgo.WithBoolean("require_enrollment"),
		mcpgo.WithNumber("timeout_seconds", mcpgo.Description("Deadline per check, 1–60 seconds; default 15")),
	}
	verify = append(verify, readOnly()...)
	verify = append(verify, openWorld()...)
	srv.AddTool(mcpgo.NewTool("os_install_verify", verify...), s.handleInstallVerify)
}

func (s *mcpServer) handleInstallPlan(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.installation.Plan == nil {
		return errResult(errCodeUnsupported, "installation planning is unavailable in this host; use wendy install --help"), nil
	}
	p, err := s.installation.Plan(ctx, onboarding.Options{
		DeviceType: stringParam(req, "device_type"), Carrier: stringParam(req, "carrier"), Version: stringParam(req, "version"),
		Storage: stringParam(req, "storage"), Drive: stringParam(req, "drive"), RootfsOnly: req.GetBool("rootfs_only", false),
	})
	if err != nil {
		code := errCodeInvalidArgument
		if errors.Is(err, onboarding.ErrArtifactUnavailable) {
			code = errCodeInternal
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			code = errCodeTimeout
		}
		return errResult(code, err.Error()), nil
	}
	return okResult(p), nil
}

func (s *mcpServer) handleInstallDrives(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.installation.Drives == nil {
		return errResult(errCodeUnsupported, "host drive enumeration is unavailable"), nil
	}
	drives, err := s.installation.Drives()
	if err != nil {
		return errResult(errCodeInternal, err.Error()), nil
	}
	return okList("drives", drives), nil
}

func (s *mcpServer) handleInstallVerify(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if os.Getenv("WENDY_AGENT_SOCKET") != "" {
		return errResult(errCodeUnsupported, "verify from a host MCP session without WENDY_AGENT_SOCKET overriding the explicit target"), nil
	}
	v, err := onboarding.Verify(ctx, onboarding.VerifyOptions{
		Address: stringParam(req, "address"), OSVersion: stringParam(req, "expected_os_version"),
		DeviceType: stringParam(req, "expected_device_type"), PublicKey: stringParam(req, "expected_public_key"),
		RequireEnrollment: req.GetBool("require_enrollment", false), Timeout: time.Duration(intParam(req, "timeout_seconds", 15)) * time.Second,
	}, s.connectFn)
	if err != nil {
		return errResult(errCodeInvalidArgument, err.Error()), nil
	}
	r := okResult(v)
	r.IsError = !v.Verified
	return r, nil
}
