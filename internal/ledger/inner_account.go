package ledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/axiomesh/axiom-kit/types"
)

type InnerAccount struct {
	Nonce       uint64      `json:"nonce"`
	Balance     *big.Int    `json:"balance"`
	CodeHash    []byte      `json:"code_hash"`
	StorageRoot common.Hash `json:"storage_root"`
}

func (o *InnerAccount) String() string {
	return fmt.Sprintf("{nonce: %d, balance: %v, code_hash: %v, storage_root: %v}", o.Nonce, o.Balance, types.NewHash(o.CodeHash), o.StorageRoot)
}

// Marshal Marshal the account into byte
func (o *InnerAccount) Marshal() ([]byte, error) {
	obj := &InnerAccount{
		Nonce:       o.Nonce,
		Balance:     o.Balance,
		CodeHash:    o.CodeHash,
		StorageRoot: o.StorageRoot,
	}

	return json.Marshal(obj)
}

// Unmarshal Unmarshal the account byte into structure
func (o *InnerAccount) Unmarshal(data []byte) error {
	return json.Unmarshal(data, o)
}

func InnerAccountChanged(account0 *InnerAccount, account1 *InnerAccount) bool {
	// If account1 is nil, the account does not change whatever account0 is.
	if account1 == nil {
		return false
	}

	// If account already exists, account0 is not nil. We should compare account0 and account1 to get the result.
	if account0 != nil &&
		account0.Nonce == account1.Nonce &&
		account0.Balance.Cmp(account1.Balance) == 0 &&
		bytes.Equal(account0.CodeHash, account1.CodeHash) &&
		account0.StorageRoot == account1.StorageRoot {
		return false
	}

	return true
}

func CopyOrNewIfEmpty(o *InnerAccount) *InnerAccount {
	if o == nil {
		return &InnerAccount{Balance: big.NewInt(0)}
	}

	return &InnerAccount{
		Nonce:       o.Nonce,
		Balance:     o.Balance,
		CodeHash:    o.CodeHash,
		StorageRoot: o.StorageRoot,
	}
}
