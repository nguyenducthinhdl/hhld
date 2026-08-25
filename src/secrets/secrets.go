// Package secrets loads venue credentials from the environment and enforces P11 gates.
// Strategy/risk/pnl must not import this package for trading math.
package secrets

import (
	"fmt"
	"os"
	"strings"
)

const (
	EnvLocal    = "local"
	EnvStaging  = "staging"
	EnvTestnet  = "testnet"
	EnvProd     = "prod"
	EnvMainnet  = "mainnet"
	KillEnv     = "HHLD_KILL"
	VenueEnvVar = "HHLD_VENUE_ENV"
	LiveOrders  = "HHLD_LIVE_ORDERS"
	KillFile    = "hhld.kill"

	HLAccountAddress    = "HL_ACCOUNT_ADDRESS"
	HLTestnetPrivateKey = "HL_TESTNET_PRIVATE_KEY"
	HLMainnetPrivateKey = "HL_MAINNET_PRIVATE_KEY"
)

var liveKeyVars = []string{
	HLTestnetPrivateKey,
	HLMainnetPrivateKey,
	"GRVT_STAGING_API_KEY",
	"GRVT_TESTNET_API_KEY",
	"GRVT_PROD_API_KEY",
	"GRVT_STAGING_SIGNING_KEY",
	"GRVT_TESTNET_SIGNING_KEY",
	"GRVT_PROD_SIGNING_KEY",
	"GRVT_COOKIE",
	"GRAVITY_COOKIE",
}

// HLAuth is Hyperliquid agent credentials for a venue env.
type HLAuth struct {
	AccountAddress string // master / account address for orderStatus
	PrivateKeyHex  string // agent wallet hex (0x-prefixed or bare)
	Testnet        bool
}

// NormalizeEnv maps aliases; empty becomes local.
func NormalizeEnv(env string) string {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "", EnvLocal:
		return EnvLocal
	case EnvStaging:
		return EnvStaging
	case EnvTestnet:
		return EnvTestnet
	case EnvProd, "production":
		return EnvProd
	case EnvMainnet:
		return EnvMainnet
	default:
		return strings.ToLower(strings.TrimSpace(env))
	}
}

// LiveKeyPresent is true if any venue private/API key or cookie is set.
func LiveKeyPresent() bool {
	for _, k := range liveKeyVars {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return true
		}
	}
	return false
}

// RequireNoLiveKeys errors when any venue private/API key or cookie is set.
// Paper-live and -trade-local call this so those processes never load signing keys.
func RequireNoLiveKeys() error {
	if LiveKeyPresent() {
		return fmt.Errorf("secrets: this process refuses to start with live venue keys in the environment")
	}
	return nil
}

// RefuseLocal errors when env is local and live keys are in the process environment.
func RefuseLocal(env string) error {
	if NormalizeEnv(env) != EnvLocal {
		return nil
	}
	return RequireNoLiveKeys()
}

// EnvPin checks HHLD_VENUE_ENV against the CLI flag (empty env var is allowed).
func EnvPin(flagEnv string) error {
	flagEnv = NormalizeEnv(flagEnv)
	got := strings.TrimSpace(os.Getenv(VenueEnvVar))
	if got == "" {
		return nil
	}
	if NormalizeEnv(got) != flagEnv {
		return fmt.Errorf("secrets: HHLD_VENUE_ENV=%s does not match -env %s", got, flagEnv)
	}
	return nil
}

// KillEngaged is true when HHLD_KILL=1 or cwd contains hhld.kill.
func KillEngaged() bool {
	v := strings.TrimSpace(os.Getenv(KillEnv))
	if v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	if _, err := os.Stat(KillFile); err == nil {
		return true
	}
	return false
}

// LiveOrdersEnabled is true when HHLD_LIVE_ORDERS=1 (required for venue PlaceOrder).
func LiveOrdersEnabled() bool {
	v := strings.TrimSpace(os.Getenv(LiveOrders))
	return v == "1" || strings.EqualFold(v, "true")
}

// RequireLiveOrders errors when HHLD_LIVE_ORDERS is not enabled.
func RequireLiveOrders() error {
	if !LiveOrdersEnabled() {
		return fmt.Errorf("secrets: HHLD_LIVE_ORDERS=1 required for venue PlaceOrder")
	}
	return nil
}

// HLTestnetEnv is true for staging or testnet (both use HL testnet hosts + HL_TESTNET_PRIVATE_KEY).
func HLTestnetEnv(env string) bool {
	switch NormalizeEnv(env) {
	case EnvStaging, EnvTestnet:
		return true
	default:
		return false
	}
}

// VenueWritesImplemented is true for local fake place and HL testnet/staging live place.
func VenueWritesImplemented(env string) bool {
	e := NormalizeEnv(env)
	return e == EnvLocal || HLTestnetEnv(e)
}

// LoadHLAuth loads Hyperliquid credentials for env. Testnet/staging use HL_TESTNET_*;
// mainnet/prod use HL_MAINNET_* (loader only — this slice refuses mainnet place elsewhere).
func LoadHLAuth(env string) (HLAuth, error) {
	env = NormalizeEnv(env)
	addr := strings.TrimSpace(os.Getenv(HLAccountAddress))
	if addr == "" {
		return HLAuth{}, fmt.Errorf("secrets: %s required", HLAccountAddress)
	}
	switch env {
	case EnvStaging, EnvTestnet:
		key := strings.TrimSpace(os.Getenv(HLTestnetPrivateKey))
		if key == "" {
			return HLAuth{}, fmt.Errorf("secrets: %s required for -env %s", HLTestnetPrivateKey, env)
		}
		if strings.TrimSpace(os.Getenv(HLMainnetPrivateKey)) != "" {
			return HLAuth{}, fmt.Errorf("secrets: refuse %s when using HL testnet env", HLMainnetPrivateKey)
		}
		return HLAuth{AccountAddress: addr, PrivateKeyHex: key, Testnet: true}, nil
	case EnvMainnet, EnvProd:
		key := strings.TrimSpace(os.Getenv(HLMainnetPrivateKey))
		if key == "" {
			return HLAuth{}, fmt.Errorf("secrets: %s required for -env %s", HLMainnetPrivateKey, env)
		}
		return HLAuth{AccountAddress: addr, PrivateKeyHex: key, Testnet: false}, nil
	default:
		return HLAuth{}, fmt.Errorf("secrets: no HL auth for env %s", env)
	}
}
