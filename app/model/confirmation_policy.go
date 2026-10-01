package model

import (
	"fmt"
	"io"
	"strconv"
)

const (
	ConfirmationDepthTron ConfKey = "confirmation_depth_tron"
	ConfirmationDepthEthereum ConfKey = "confirmation_depth_ethereum"
	ConfirmationDepthBsc ConfKey = "confirmation_depth_bsc"
	ConfirmationDepthPolygon ConfKey = "confirmation_depth_polygon"
	ConfirmationDepthArbitrum ConfKey = "confirmation_depth_arbitrum"
	ConfirmationDepthBase ConfKey = "confirmation_depth_base"
	ConfirmationDepthXlayer ConfKey = "confirmation_depth_xlayer"
	ConfirmationDepthPlasma ConfKey = "confirmation_depth_plasma"
)

type confirmationRule struct {
	network string
	key ConfKey
	minimum int64
}

var confirmationRules = []confirmationRule{
	{"tron", ConfirmationDepthTron, 20},
	{"ethereum", ConfirmationDepthEthereum, 12},
	{"bsc", ConfirmationDepthBsc, 15},
	{"polygon", ConfirmationDepthPolygon, 40},
	{"arbitrum", ConfirmationDepthArbitrum, 40},
	{"base", ConfirmationDepthBase, 40},
	{"xlayer", ConfirmationDepthXlayer, 12},
	{"plasma", ConfirmationDepthPlasma, 40},
}

// Depth counts successor blocks: observed head minus transaction inclusion height.
// Invalid configuration always fails closed; there is no production bypass.
func RequiredConfirmationDepth(network string) (int64, error) {
	for _, rule := range confirmationRules {
		if rule.network != network { continue }
		raw := GetC(rule.key)
		if raw == "" { raw = defaultConf[rule.key] }
		depth, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || depth < rule.minimum {
			return 0, fmt.Errorf("%s confirmation depth must be an integer >= %d", network, rule.minimum)
		}
		return depth, nil
	}
	return 0, fmt.Errorf("no confirmation policy for network %q", network)
}

func ConfirmationDepthReached(network string, head, inclusion int64) (bool, error) {
	required, err := RequiredConfirmationDepth(network)
	if err != nil { return false, err }
	if head <= 0 || inclusion <= 0 || head < inclusion { return false, nil }
	return head-inclusion >= required, nil
}

// Legacy 0/1 values are accepted only for upgrade compatibility, never as bypasses.
func ValidateConfirmationPolicy(output io.Writer) error {
	legacy := GetC(BlockOffsetConfirm)
	switch legacy {
	case "", "1":
	case "0":
		fmt.Fprintln(output, "WARNING: legacy block_offset_confirm=0 is ignored; per-chain confirmation protection is enforced")
	default:
		return fmt.Errorf("invalid legacy block_offset_confirm; expected 0 or 1")
	}
	for _, rule := range confirmationRules {
		depth, err := RequiredConfirmationDepth(rule.network)
		if err != nil { return err }
		fmt.Fprintf(output, "confirmation policy: network=%s successor_blocks=%d minimum=%d enforced=true\n", rule.network, depth, rule.minimum)
	}
	return nil
}
