package distribution

import (
	"context"
	"fmt"
	"io"
)

// ValidateCachedRelease rechecks the publisher's exact tag/commit and current
// checksum manifest. A local receipt alone never proves that a mutable release
// still identifies the same artifact.
func (c *Client) ValidateCachedRelease(ctx context.Context, b Build) error {
	if b.Channel != "release" || !safeRef.MatchString(b.Ref) {
		return fmt.Errorf("distribution: exact cached release required")
	}
	r, err := c.release(ctx, b.Ref)
	if err != nil {
		return err
	}
	if b.Version != r.Tag {
		return fmt.Errorf("distribution: cached release version differs")
	}
	commit, err := c.resolveCommit(ctx, r.Tag)
	if err != nil || commit != b.Commit {
		return fmt.Errorf("distribution: cached release commit differs")
	}
	asset, err := c.asset(r, "checksums.txt", 64<<10)
	if err != nil {
		return err
	}
	response, err := c.get(ctx, asset.URL)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return fmt.Errorf("distribution: invalid checksum manifest")
	}
	sum, err := checksumFor(string(body), "wg-guard_linux_"+c.options.Arch)
	if err != nil || sum != b.SHA256 {
		return fmt.Errorf("distribution: cached release checksum differs")
	}
	return nil
}
