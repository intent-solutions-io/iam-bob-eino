package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/patch"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/verify"
)

// --- apply_patch (R3) --------------------------------------------------------

type applyPatchInput struct {
	PatchJSON string `json:"patch_json" jsonschema_description:"An intent-bob-eino-patch/v1 JSON document: {schema_version, files:[{path, pre_sha256, hunks:[{find, replace, expect_count, occurrence}]}]}. Literal find/replace on existing text files; pre_sha256 is the hex sha256 of each file's full current content and is verified; expect_count must equal the exact number of occurrences of find; occurrence 0 replaces all, N replaces only the N-th."`
}

func newApplyPatch(g *governor.Governor) (tool.InvokableTool, error) {
	return utils.InferTool("apply_patch",
		"Apply a governed intent-bob-eino-patch/v1 document (literal find/replace hunks with verified pre-hashes and exact occurrence counts) atomically to existing workspace files. Disabled unless writes are enabled; requires approval; two-phase with rollback; the result reports hashes and counts only.",
		func(ctx context.Context, in *applyPatchInput) (string, error) {
			p, perr := patch.Parse([]byte(in.PatchJSON))
			if perr == nil {
				perr = patch.Validate(p)
			}

			// Asset/Summary identify the exact change surface so the approval
			// is over specific files, not a blank patch.
			asset := "(malformed patch)"
			var paths []string
			if perr == nil {
				for _, fc := range p.Files {
					paths = append(paths, fc.Path)
				}
				asset = strings.Join(paths, ",")
			}
			spec := governor.ActionSpec{
				Tool: "apply_patch", Risk: policy.R3, Asset: asset, Assets: paths,
				Summary: fmt.Sprintf("apply patch to [%s] (%d bytes of patch)", asset, len(in.PatchJSON)),
				RawArgs: jsonOf(in),
			}
			t := g.Begin(spec)
			defer t.EnsureEmitted(ctx)

			if perr != nil {
				// A malformed document is an argument error, not a governance
				// denial — record it honestly.
				t.FinishError(ctx, perr)
				return "ERROR: " + perr.Error(), nil
			}

			// Same guard ladder as write_file, per target file, BEFORE any
			// authorization: workspace containment, secret names, .git.
			for _, fc := range p.Files {
				if err := g.WS.Check(fc.Path); err != nil {
					t.FinishDenied(ctx, err.Error())
					return "DENIED: " + err.Error(), nil
				}
				if isSecretPath(fc.Path) {
					t.FinishDenied(ctx, "refused: looks like a secret file: "+fc.Path)
					return "DENIED: refusing to patch a likely secret file", nil
				}
				if isDotGitPath(fc.Path) {
					t.FinishDenied(ctx, "refused: patching into .git is not allowed")
					return "DENIED: refusing to patch inside the .git directory", nil
				}
			}

			if gate := t.Authorize(ctx, spec); !gate.Allowed {
				t.FinishDenied(ctx, gate.Reason)
				return "DENIED: " + gate.Reason, nil
			}

			res, aerr := patch.Apply(g.WS, p)
			if aerr != nil {
				t.FinishError(ctx, aerr)
				return "ERROR: " + aerr.Error(), nil
			}

			// patch.Apply already re-read and hash-verified every file; carry
			// that as the independent verification verdict.
			hunks := 0
			for _, fr := range res.Files {
				hunks += fr.HunksApplied
			}
			v := verify.Verdict{Verified: true, Info: fmt.Sprintf("post-write re-read hash-verified %d file(s)", len(res.Files))}
			t.Finish(ctx, "ok", fmt.Sprintf("patched %d file(s), %d hunk(s)", len(res.Files), hunks), v)
			return jsonOf(res), nil
		})
}
