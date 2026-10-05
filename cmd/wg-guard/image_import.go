package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
)

// Import is an explicit offline artifact operation. It loads Docker's cache;
// installation/update still admits the image and coordinates active changes.
func runImageImport(args []string) error {
	fs := flag.NewFlagSet("image-import", flag.ContinueOnError)
	archive := fs.String("archive", "", "verified runtime archive")
	metadata := fs.String("runtime-metadata", "", "verified runtime-metadata.json")
	buildMetadata := fs.String("build-metadata", "", "private receipt for the verified matching manager")
	core := fs.String("core", "recommended", "reviewed core bundle")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *archive == "" || *metadata == "" || *buildMetadata == "" {
		return fmt.Errorf("image-import requires --archive, --runtime-metadata and --build-metadata")
	}
	h := install.NewRealHost()
	if !h.IsRoot() {
		return fmt.Errorf("image-import: root required")
	}
	file, err := os.Open(*metadata)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	_ = file.Close()
	if err != nil || len(raw) > 64<<10 {
		return fmt.Errorf("image-import: invalid runtime metadata size")
	}
	var manifest distribution.RuntimeManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("image-import: invalid runtime metadata")
	}
	ctx := context.Background()
	b, _, cleanup, err := prepareBuild(ctx, distribution.Selection{}, *buildMetadata)
	defer cleanup()
	if err != nil {
		return err
	}
	if err := install.VerifyManagerBuild(ctx, h, b); err != nil {
		return err
	}
	bundle, err := install.SelectCore(*core)
	if err != nil {
		return err
	}
	if err := install.LoadRuntimeImage(ctx, h, b, manifest, bundle, *archive); err != nil {
		return err
	}
	fmt.Println(manifest.ImageID)
	return nil
}
