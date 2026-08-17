package config

import (
	"fmt"
	"strings"
)

func validatePatchPolicy(errs *[]error, section HotUpdateSection, productionLike bool) {
	if section.ApplierKind != "native_patch" {
		return
	}
	for idx, target := range section.NativePatchAllowedTargets {
		requireSafeName(errs, fmt.Sprintf("hot_update.native_patch_allowed_targets[%d]", idx), target)
	}
	for idx, symbol := range section.PatchOldSymbols {
		field := fmt.Sprintf("hot_update.native_patch_allowed_old_symbols[%d]", idx)
		requireString(errs, field, symbol)
		if strings.TrimSpace(symbol) != symbol || strings.ContainsAny(symbol, "\r\n") {
			*errs = append(*errs, fmt.Errorf("%s cannot contain surrounding whitespace or line breaks", field))
		}
	}
	if !productionLike {
		if section.NativePatchMaxLiveSeconds <= 0 {
			*errs = append(*errs, fmt.Errorf("hot_update.native_patch_max_live_seconds is required for native patch"))
		}
		return
	}
	if !section.RequireSignature {
		*errs = append(*errs, fmt.Errorf("hot_update.require_signature is required for production-like native patch"))
	}
	if strings.TrimSpace(section.SigningKeyEnv) == "" {
		*errs = append(*errs, fmt.Errorf("hot_update.signing_key_env is required for production-like native patch"))
	}
	if !section.NativePatchRequireBuildID {
		*errs = append(*errs, fmt.Errorf("hot_update.native_patch_require_build_id is required for production-like native patch"))
	}
	if !section.PatchRequireActor {
		*errs = append(*errs, fmt.Errorf("hot_update.native_patch_require_requested_by is required for production-like native patch"))
	}
	if !section.NativePatchRequireReason {
		*errs = append(*errs, fmt.Errorf("hot_update.native_patch_require_reason is required for production-like native patch"))
	}
	if section.NativePatchMinReasonLength <= 0 {
		*errs = append(*errs, fmt.Errorf("hot_update.native_patch_min_reason_length is required for production-like native patch"))
	}
	if len(section.PatchOldSymbols) == 0 {
		*errs = append(*errs, fmt.Errorf("hot_update.native_patch_allowed_old_symbols requires at least one value for production-like native patch"))
	}
	if section.NativePatchMaxLiveSeconds <= 0 {
		*errs = append(*errs, fmt.Errorf("hot_update.native_patch_max_live_seconds is required for production-like native patch"))
	}
	if section.NativePatchMaxLiveSeconds > 3600 {
		*errs = append(*errs, fmt.Errorf("hot_update.native_patch_max_live_seconds must be <= 3600 for production-like native patch"))
	}
}
