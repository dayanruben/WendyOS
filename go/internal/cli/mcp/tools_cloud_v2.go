package mcp

import (
	"context"
	"fmt"
	"io"

	"github.com/wendylabsinc/wendy/go/internal/shared/certs"
	"github.com/wendylabsinc/wendy/go/internal/shared/config"
	cloudpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/cloudpb/v2"
)

func int32Ptr(v int32) *int32 { return &v }

// mcpListCloudAssetsV2 lists compute-device assets on the v2 AssetService,
// tenant-scoped by the session's SPIFFE principal. The v1 mcpListCloudAssets
// sends cert.OrganizationID, which is 0 for a v2 (UUID-identity) session, so it
// silently queried org 0 and returned nothing (WDY-3146). Mirrors the CLI's
// commands/fetchCloudAssetsV2, reimplemented here because the mcp package
// cannot import cli/commands.
func mcpListCloudAssetsV2(ctx context.Context, auth *config.AuthConfig, filter string, onlineOnly bool) ([]*cloudpbv2.Asset, error) {
	identity, err := certs.ParsePrincipal(auth.Certificates[0].PrincipalURI)
	if err != nil {
		return nil, fmt.Errorf("reading cloud organization: %w", err)
	}
	conn, err := mcpDialCloudGRPC(auth)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	client := cloudpbv2.NewAssetServiceClient(conn)
	const maxAssets = 10_000
	const pageSize = 200
	assets := make([]*cloudpbv2.Asset, 0)
	for offset := int32(0); ; {
		req := &cloudpbv2.ListAssetsRequest{
			OrganizationId:  identity.TenantUUID,
			IsComputeDevice: boolPtr(true),
			Offset:          int32Ptr(offset),
			Limit:           int32Ptr(pageSize),
		}
		if filter != "" {
			req.Filter = &filter
		}
		if onlineOnly {
			req.OnlineOnly = boolPtr(true)
		}
		cloudCtx, err := mcpCloudContext(ctx, auth)
		if err != nil {
			return nil, err
		}
		stream, err := client.ListAssets(cloudCtx, req)
		if err != nil {
			return nil, fmt.Errorf("listing devices: %w", err)
		}
		var page, total int32
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("listing devices: %w", err)
			}
			if len(assets) >= maxAssets {
				return nil, &cloudResolveErr{code: errCodeInvalidArgument, msg: fmt.Sprintf("cloud returned more than %d devices", maxAssets)}
			}
			assets = append(assets, resp.GetAsset())
			page++
			total = resp.GetTotal()
		}
		offset += page
		if page == 0 || offset >= total {
			return assets, nil
		}
	}
}

// cloudAssetV2ToMap renders a v2 asset for the MCP discover tool. Per sem's
// "device ID, not asset ID" rule the identity emitted is the device id (the v2
// asset's UUID), labelled device_id, alongside device_name — never an int32
// asset id.
func cloudAssetV2ToMap(a *cloudpbv2.Asset) map[string]any {
	out := map[string]any{
		"device_id":         a.GetId(),
		"device_name":       a.GetName(),
		"organization_id":   a.GetOrganizationId(),
		"asset_type":        a.GetAssetType(),
		"is_compute_device": a.GetIsComputeDevice(),
	}
	if a.DeviceType != nil {
		out["device_type"] = a.GetDeviceType()
	}
	if a.Architecture != nil {
		out["architecture"] = a.GetArchitecture()
	}
	if a.OsVersion != nil {
		out["os_version"] = a.GetOsVersion()
	}
	return out
}
