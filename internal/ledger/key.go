package ledger

import (
	"crypto/sha256"
	"fmt"

	"github.com/axiomesh/axiom-kit/hexutil"
	"github.com/axiomesh/axiom-kit/types"
)

const (
	blockKey           = "block-"
	blockHashKey       = "block-hash-"
	blockHeightKey     = "block-height-"
	blockTxSetKey      = "block-tx-set-"
	interchainMetaKey  = "interchain-meta-"
	receiptKey         = "receipt-"
	transactionKey     = "tx-"
	transactionMetaKey = "tx-meta-"
	chainMetaKey       = "chain-meta"
	accountKey         = "account-"
	codeKey            = "code-"
	journalKey         = "journal-"
)

func compositeKey(prefix string, value any) []byte {
	return append([]byte(prefix), []byte(fmt.Sprintf("%v", value))...)
}

func compositeAccountKey(addr *types.Address) []byte {
	return hexutil.EncodeToNibbles(addr.String())
}

func compositeStorageKey(addr *types.Address, key []byte) []byte {
	keyHash := sha256.Sum256(append(hexutil.EncodeToNibbles(addr.String()), key...))
	return hexutil.BytesToHex(keyHash[:])
}

func compositeCodeKey(addr *types.Address, codeHash []byte) []byte {
	return append(addr.Bytes(), codeHash...)
}
