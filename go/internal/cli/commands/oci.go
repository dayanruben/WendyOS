package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/wendylabsinc/wendy/internal/cli/grpcclient"
	"github.com/wendylabsinc/wendy/proto/gen/agentpb"
)

// OCIBlob represents a content-addressable blob for an OCI image.
type OCIBlob struct {
	Digest string
	Data   []byte
}

// buildMacOSOCIImage creates an OCI image containing a macOS Swift binary
// and optionally a sandbox profile. It returns three blobs: the gzipped layer,
// the OCI config, and the OCI manifest.
func buildMacOSOCIImage(binaryPath, sandboxPath, product, arch string) (layer, config, manifest OCIBlob, err error) {
	// Create an uncompressed tar archive.
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)

	// Add binary.
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return layer, config, manifest, fmt.Errorf("reading binary: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:    "./" + product,
		Size:    int64(len(binaryData)),
		Mode:    0755,
		ModTime: time.Unix(0, 0),
	}); err != nil {
		return layer, config, manifest, fmt.Errorf("writing tar header for binary: %w", err)
	}
	if _, err := tw.Write(binaryData); err != nil {
		return layer, config, manifest, fmt.Errorf("writing binary to tar: %w", err)
	}

	// Add sandbox profile if present.
	if sandboxPath != "" {
		sbData, err := os.ReadFile(sandboxPath)
		if err != nil {
			return layer, config, manifest, fmt.Errorf("reading sandbox profile: %w", err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:    "./sandbox.sb",
			Size:    int64(len(sbData)),
			Mode:    0644,
			ModTime: time.Unix(0, 0),
		}); err != nil {
			return layer, config, manifest, fmt.Errorf("writing tar header for sandbox: %w", err)
		}
		if _, err := tw.Write(sbData); err != nil {
			return layer, config, manifest, fmt.Errorf("writing sandbox to tar: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		return layer, config, manifest, fmt.Errorf("closing tar: %w", err)
	}

	// Diff ID = SHA256 of uncompressed tar.
	uncompressedTar := tarBuf.Bytes()
	diffIDHash := sha256.Sum256(uncompressedTar)
	diffID := godigest.NewDigestFromEncoded(godigest.SHA256, hex.EncodeToString(diffIDHash[:]))

	// Gzip the tar.
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	if _, err := gw.Write(uncompressedTar); err != nil {
		return layer, config, manifest, fmt.Errorf("gzip write: %w", err)
	}
	if err := gw.Close(); err != nil {
		return layer, config, manifest, fmt.Errorf("gzip close: %w", err)
	}

	// Layer digest = SHA256 of gzipped tar.
	gzData := gzBuf.Bytes()
	layerHash := sha256.Sum256(gzData)
	layerDigest := godigest.NewDigestFromEncoded(godigest.SHA256, hex.EncodeToString(layerHash[:]))

	layer = OCIBlob{Digest: layerDigest.String(), Data: gzData}

	// Build OCI config.
	imgConfig := ocispec.Image{
		Platform: ocispec.Platform{
			Architecture: arch,
			OS:           "darwin",
		},
		RootFS: ocispec.RootFS{
			Type:    "layers",
			DiffIDs: []godigest.Digest{diffID},
		},
		Config: ocispec.ImageConfig{
			Entrypoint: []string{"./" + product},
		},
	}
	configData, err := json.Marshal(imgConfig)
	if err != nil {
		return layer, config, manifest, fmt.Errorf("marshaling config: %w", err)
	}
	configHash := sha256.Sum256(configData)
	configDigest := godigest.NewDigestFromEncoded(godigest.SHA256, hex.EncodeToString(configHash[:]))

	config = OCIBlob{Digest: configDigest.String(), Data: configData}

	// Build OCI manifest.
	mf := ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Config: ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageConfig,
			Digest:    configDigest,
			Size:      int64(len(configData)),
		},
		Layers: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageLayerGzip,
				Digest:    layerDigest,
				Size:      int64(len(gzData)),
			},
		},
	}
	mf.SchemaVersion = 2
	manifestData, err := json.Marshal(mf)
	if err != nil {
		return layer, config, manifest, fmt.Errorf("marshaling manifest: %w", err)
	}
	manifestHash := sha256.Sum256(manifestData)
	manifestDigest := godigest.NewDigestFromEncoded(godigest.SHA256, hex.EncodeToString(manifestHash[:]))

	manifest = OCIBlob{Digest: manifestDigest.String(), Data: manifestData}

	return layer, config, manifest, nil
}

// uploadBlob streams a blob to the device agent via the WriteLayer RPC.
// The digest uses standard OCI format: "sha256:<hex>".
func uploadBlob(ctx context.Context, conn *grpcclient.AgentConnection, digest string, data []byte) error {
	stream, err := conn.ContainerService.WriteLayer(ctx)
	if err != nil {
		return fmt.Errorf("opening WriteLayer stream: %w", err)
	}

	const chunkSize = 64 * 1024
	for offset := 0; offset < len(data); offset += chunkSize {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}

		req := &agentpb.WriteLayerRequest{
			Data: data[offset:end],
		}
		if offset == 0 {
			req.Digest = digest
		}

		if err := stream.Send(req); err != nil {
			return fmt.Errorf("sending chunk: %w", err)
		}
	}

	if err := stream.CloseSend(); err != nil {
		return fmt.Errorf("closing send: %w", err)
	}

	for {
		if _, err := stream.Recv(); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("receiving response: %w", err)
		}
	}

	return nil
}
