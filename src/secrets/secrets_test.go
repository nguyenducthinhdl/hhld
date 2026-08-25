package secrets_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nguyenducthinhdl/hhld/src/secrets"
)

func TestRefuseLocal_LiveKey(t *testing.T) {
	t.Setenv("HL_TESTNET_PRIVATE_KEY", "0xabc")
	if err := secrets.RefuseLocal("local"); err == nil {
		t.Fatal("want refuse")
	}
	if err := secrets.RefuseLocal("staging"); err != nil {
		t.Fatal(err)
	}
}

func TestEnvPin(t *testing.T) {
	t.Setenv("HHLD_VENUE_ENV", "local")
	if err := secrets.EnvPin("local"); err != nil {
		t.Fatal(err)
	}
	if err := secrets.EnvPin("staging"); err == nil {
		t.Fatal("want mismatch")
	}
	t.Setenv("HHLD_VENUE_ENV", "")
	if err := secrets.EnvPin("local"); err != nil {
		t.Fatal(err)
	}
}

func TestKillEngaged(t *testing.T) {
	t.Setenv("HHLD_KILL", "1")
	if !secrets.KillEngaged() {
		t.Fatal("env")
	}
	t.Setenv("HHLD_KILL", "")
	dir := t.TempDir()
	t.Chdir(dir)
	if secrets.KillEngaged() {
		t.Fatal("want false")
	}
	if err := os.WriteFile(filepath.Join(dir, "hhld.kill"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !secrets.KillEngaged() {
		t.Fatal("file")
	}
}

func TestVenueWritesImplemented(t *testing.T) {
	if !secrets.VenueWritesImplemented("local") {
		t.Fatal("local")
	}
	if !secrets.VenueWritesImplemented("staging") {
		t.Fatal("staging HL live")
	}
	if !secrets.VenueWritesImplemented("testnet") {
		t.Fatal("testnet HL live")
	}
	if secrets.VenueWritesImplemented("prod") {
		t.Fatal("prod not in this slice")
	}
}

func TestLiveOrdersEnabled(t *testing.T) {
	t.Setenv(secrets.LiveOrders, "")
	if secrets.LiveOrdersEnabled() {
		t.Fatal("want false")
	}
	if err := secrets.RequireLiveOrders(); err == nil {
		t.Fatal("want require")
	}
	t.Setenv(secrets.LiveOrders, "1")
	if !secrets.LiveOrdersEnabled() {
		t.Fatal("want true")
	}
	if err := secrets.RequireLiveOrders(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadHLAuth_Testnet(t *testing.T) {
	t.Setenv(secrets.HLAccountAddress, "0xabc")
	t.Setenv(secrets.HLTestnetPrivateKey, "0xdead")
	t.Setenv(secrets.HLMainnetPrivateKey, "")
	a, err := secrets.LoadHLAuth("testnet")
	if err != nil || !a.Testnet || a.PrivateKeyHex != "0xdead" {
		t.Fatalf("%+v %v", a, err)
	}
	t.Setenv(secrets.HLMainnetPrivateKey, "0xmain")
	if _, err := secrets.LoadHLAuth("staging"); err == nil {
		t.Fatal("want refuse mainnet key on testnet env")
	}
}

func TestRequireNoLiveKeys(t *testing.T) {
	t.Setenv("HL_MAINNET_PRIVATE_KEY", "")
	t.Setenv("HL_TESTNET_PRIVATE_KEY", "")
	t.Setenv("GRVT_STAGING_API_KEY", "")
	t.Setenv("GRVT_TESTNET_API_KEY", "")
	t.Setenv("GRVT_PROD_API_KEY", "")
	t.Setenv("GRVT_STAGING_SIGNING_KEY", "")
	t.Setenv("GRVT_TESTNET_SIGNING_KEY", "")
	t.Setenv("GRVT_PROD_SIGNING_KEY", "")
	t.Setenv("GRVT_COOKIE", "")
	t.Setenv("GRAVITY_COOKIE", "")
	if err := secrets.RequireNoLiveKeys(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRVT_COOKIE", "x")
	if err := secrets.RequireNoLiveKeys(); err == nil {
		t.Fatal("want refuse")
	}
}

func TestNormalizeEnv(t *testing.T) {
	if secrets.NormalizeEnv("") != secrets.EnvLocal {
		t.Fatal("empty")
	}
	if secrets.NormalizeEnv("production") != secrets.EnvProd {
		t.Fatal("production alias")
	}
}
