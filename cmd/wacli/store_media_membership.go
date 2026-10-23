package main

import (
	"fmt"

	"github.com/openclaw/wacli/internal/mediamembership"
	"github.com/openclaw/wacli/internal/out"
	"github.com/spf13/cobra"
)

func newStoreVerifyMediaCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "verify-media",
		Short: "Verify every stored local media reference into temporary immutable objects (Linux)",
		Long:  "Verify every stored local media reference without network access or modifying originals. Missing bytes, missing fingerprints, unsupported kernels and resource limits fail the entire check. Temporary sealed objects are discarded after the report; this command does not capture, publish or certify a whole-store boundary.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := withTimeout(cmd.Context(), flags)
			defer cancel()
			readFlags := *flags
			readFlags.readOnly = true
			a, _, err := newApp(ctx, &readFlags, false, false)
			if err != nil {
				return err
			}
			defer a.Close()
			members, err := a.DB().EnumerateMediaMembership(ctx)
			if err != nil {
				return err
			}
			refs := make([]mediamembership.Reference, len(members))
			for i, member := range members {
				refs[i] = mediamembership.Reference{Key: member.Key, LocalPath: member.LocalPath,
					ExpectedSHA256: member.ExpectedSHA256, ExpectedLength: member.ExpectedLength}
			}
			blobs, err := mediamembership.Stage(ctx, refs)
			if err != nil {
				return err
			}
			defer func() {
				for _, blob := range blobs {
					_ = blob.Close()
				}
			}()
			var total int64
			for _, blob := range blobs {
				total += blob.Size
			}
			if flags.asJSON {
				return out.WriteJSON(cmd.OutOrStdout(), map[string]any{
					"status": "verified-byte-objects-only", "checked_references": len(blobs),
					"staged_bytes": total, "boundary": "UNVERIFIED", "complete": false,
					"capture_enabled": false, "publishable": false, "windows_validated": false,
				})
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Verified %d local references (%d bytes) into temporary sealed objects. Whole-store boundary UNVERIFIED; no capture published.\n", len(blobs), total)
			return err
		},
	}
}
