package hyperliquid

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/vmihailenco/msgpack/v5"
)

// Signature is the EIP-712 split signature for POST /exchange.
type Signature struct {
	R string `json:"r"`
	S string `json:"s"`
	V int    `json:"v"`
}

func parsePrivateKey(hexKey string) (*ecdsa.PrivateKey, error) {
	hexKey = strings.TrimSpace(hexKey)
	hexKey = strings.TrimPrefix(hexKey, "0x")
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: private key: %w", err)
	}
	pk, err := crypto.ToECDSA(b)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: private key: %w", err)
	}
	return pk, nil
}

func actionHash(action any, vaultAddress string, nonce int64) ([]byte, error) {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	// Do not SetSortMapKeys — structs serialize in field order (match Python SDK).
	enc.UseCompactInts(true)
	if err := enc.Encode(action); err != nil {
		return nil, fmt.Errorf("hyperliquid: msgpack action: %w", err)
	}
	data := buf.Bytes()

	nonceBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(nonceBytes, uint64(nonce))
	data = append(data, nonceBytes...)

	if vaultAddress == "" {
		data = append(data, 0x00)
	} else {
		data = append(data, 0x01)
		addr := strings.TrimPrefix(strings.TrimSpace(vaultAddress), "0x")
		ab, err := hex.DecodeString(addr)
		if err != nil || len(ab) != 20 {
			return nil, fmt.Errorf("hyperliquid: vault address")
		}
		data = append(data, ab...)
	}
	return crypto.Keccak256(data), nil
}

func signL1Action(pk *ecdsa.PrivateKey, action any, vaultAddress string, nonce int64, isMainnet bool) (Signature, error) {
	hash, err := actionHash(action, vaultAddress, nonce)
	if err != nil {
		return Signature{}, err
	}
	source := "b"
	if isMainnet {
		source = "a"
	}
	chainID := math.HexOrDecimal256(*big.NewInt(1337))
	typed := apitypes.TypedData{
		Domain: apitypes.TypedDataDomain{
			ChainId:           &chainID,
			Name:              "Exchange",
			Version:           "1",
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Types: apitypes.Types{
			"Agent": []apitypes.Type{
				{Name: "source", Type: "string"},
				{Name: "connectionId", Type: "bytes32"},
			},
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
		},
		PrimaryType: "Agent",
		Message: map[string]any{
			"source":       source,
			"connectionId": hash,
		},
	}
	return signTyped(pk, typed)
}

func signTyped(pk *ecdsa.PrivateKey, typed apitypes.TypedData) (Signature, error) {
	domainSep, err := typed.HashStruct("EIP712Domain", typed.Domain.Map())
	if err != nil {
		return Signature{}, fmt.Errorf("hyperliquid: domain hash: %w", err)
	}
	msgHash, err := typed.HashStruct(typed.PrimaryType, typed.Message)
	if err != nil {
		return Signature{}, fmt.Errorf("hyperliquid: message hash: %w", err)
	}
	raw := []byte{0x19, 0x01}
	raw = append(raw, domainSep...)
	raw = append(raw, msgHash...)
	digest := crypto.Keccak256Hash(raw)
	sig, err := crypto.Sign(digest.Bytes(), pk)
	if err != nil {
		return Signature{}, fmt.Errorf("hyperliquid: sign: %w", err)
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := int(sig[64]) + 27
	return Signature{R: hexutil.EncodeBig(r), S: hexutil.EncodeBig(s), V: v}, nil
}
