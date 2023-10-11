package base

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	rbft "github.com/axiomesh/axiom-bft"
	"github.com/axiomesh/axiom-kit/types"
	"github.com/axiomesh/axiom-ledger/internal/executor/system/common"
	"github.com/axiomesh/axiom-ledger/internal/ledger"
	vm "github.com/axiomesh/eth-kit/evm"
)

const (
	nextEpochInfoKey          = "nextEpochInfoKey"
	historyEpochInfoKeyPrefix = "historyEpochInfoKeyPrefix"
)

var _ common.SystemContract = (*EpochManager)(nil)

type EpochManager struct {
	logger  logrus.FieldLogger
	account ledger.IAccount
}

func NewEpochManager(cfg *common.SystemContractConfig) *EpochManager {
	return &EpochManager{
		logger: cfg.Logger,
	}
}

func (m *EpochManager) Reset(lastHeight uint64, stateLedger ledger.StateLedger) {
	m.account = stateLedger.GetOrCreateAccount(types.NewAddressByStr(common.EpochManagerContractAddr))
}

func (m *EpochManager) Run(msg *vm.Message) (*vm.ExecutionResult, error) {
	// TODO: add query method
	return nil, errors.New("unsupported method")
}

func (m *EpochManager) EstimateGas(callArgs *types.CallArgs) (uint64, error) {
	return 0, errors.New("unsupported method")
}

func historyEpochInfoKey(epoch uint64) []byte {
	return []byte(fmt.Sprintf("%s_%d", historyEpochInfoKeyPrefix, epoch))
}

func InitEpochInfo(lg ledger.StateLedger, epochInfo *rbft.EpochInfo) error {
	account := lg.GetOrCreateAccount(types.NewAddressByStr(common.EpochManagerContractAddr))
	epochInfo = epochInfo.Clone()

	c, err := json.Marshal(epochInfo)
	if err != nil {
		return err
	}
	account.SetState(historyEpochInfoKey(epochInfo.Epoch), c)

	epochInfo.Epoch++
	epochInfo.StartBlock += epochInfo.EpochPeriod
	c, err = json.Marshal(epochInfo)
	if err != nil {
		return err
	}
	// set history state
	account.SetState([]byte(nextEpochInfoKey), c)
	return nil
}

func getEpoch(lg ledger.StateLedger, key []byte) (*rbft.EpochInfo, error) {
	account := lg.GetOrCreateAccount(types.NewAddressByStr(common.EpochManagerContractAddr))
	success, data := account.GetState(key)
	if success {
		var e rbft.EpochInfo
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		return &e, nil
	}
	return nil, errors.New("not found epoch info")
}

func GetNextEpochInfo(lg ledger.StateLedger) (*rbft.EpochInfo, error) {
	return getEpoch(lg, []byte(nextEpochInfoKey))
}

func GetEpochInfo(lg ledger.StateLedger, epoch uint64) (*rbft.EpochInfo, error) {
	return getEpoch(lg, historyEpochInfoKey(epoch))
}

func GetCurrentEpochInfo(lg ledger.StateLedger) (*rbft.EpochInfo, error) {
	next, err := GetNextEpochInfo(lg)
	if err != nil {
		return nil, err
	}
	return getEpoch(lg, historyEpochInfoKey(next.Epoch-1))
}

// TurnIntoNewEpoch when execute epoch last, return new current epoch info
func TurnIntoNewEpoch(lg ledger.StateLedger) (*rbft.EpochInfo, error) {
	account := lg.GetOrCreateAccount(types.NewAddressByStr(common.EpochManagerContractAddr))
	success, data := account.GetState([]byte(nextEpochInfoKey))
	if success {
		var e rbft.EpochInfo
		if err := json.Unmarshal(data, &e); err != nil {
			return nil, err
		}
		// set current epoch info
		account.SetState(historyEpochInfoKey(e.Epoch), data)

		n := e.Clone()
		n.Epoch++
		n.StartBlock += n.EpochPeriod
		c, err := json.Marshal(n)
		if err != nil {
			return nil, err
		}
		// set next epoch info
		account.SetState([]byte(nextEpochInfoKey), c)

		// return current
		return &e, nil
	}
	return nil, errors.New("not found current epoch info")
}
