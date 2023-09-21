package adaptor

import (
	"fmt"

	"github.com/axiomesh/axiom/internal/block_sync"
	"github.com/sirupsen/logrus"

	"github.com/axiomesh/axiom-bft/common/consensus"
	rbfttypes "github.com/axiomesh/axiom-bft/types"
	"github.com/axiomesh/axiom-kit/types"
	"github.com/axiomesh/axiom/internal/order/common"
)

func (s *RBFTAdaptor) Execute(requests []*types.Transaction, localList []bool, seqNo uint64, timestamp int64, proposerAccount string) {
	s.ReadyC <- &Ready{
		Txs:             requests,
		LocalList:       localList,
		Height:          seqNo,
		Timestamp:       timestamp,
		ProposerAccount: proposerAccount,
	}
}

func (s *RBFTAdaptor) StateUpdate(seqNo uint64, digest string, checkpoints []*consensus.SignedCheckpoint, epochChanges ...*consensus.EpochChange) {
	s.StateUpdating = true
	s.StateUpdateHeight = seqNo

	peers := make([]string, 0)

	chain := s.getChainMetaFunc()
	currentHeight := chain.Height
	curBlockHash := chain.BlockHash.String()

	// if epochChanges is not nil, it means that the epoch has changed, so we need to get the peers from the latest epochChanges
	if epochChanges != nil {
		peers = epochChanges[len(epochChanges)-1].Validators
		lowHeight := epochChanges[0].Checkpoint.Height()
		if currentHeight > lowHeight {

		}
	} else {
		for _, v := range s.EpochInfo.ValidatorSet {
			if v.AccountAddress != s.config.SelfAccountAddress {
				peers = append(peers, v.P2PNodeID)
			}
		}
	}

	currentHeight = chain.Height

	// if this node have invalid local state, it means that this node is a Byzantine node, so we need to panic
	err := s.checkLocalState(currentHeight, checkpoints, epochChanges...)
	if err != nil {
		panic(err)
	}

	syncer, err := block_sync.New(s.logger, peers, curBlockHash, currentHeight, seqNo, s.peerMgr, s.getBlockFunc, checkpoints[0].GetCheckpoint(), epochChanges...)
	if err != nil {
		panic(fmt.Errorf("block sync failed, err:%w", err))
	}

	for {
		select {
		case <-syncer.SyncDone():
			s.logger.WithFields(logrus.Fields{
				"target":      seqNo,
				"target_hash": digest,
			}).Info("State update finished fetch blocks")
			return
		case blockCache := <-syncer.Commit():
			for _, block := range blockCache {
				if block == nil {
					s.logger.Error("Receive a nil block")
					return
				}
				localList := make([]bool, len(block.Transactions))
				for i := 0; i < len(block.Transactions); i++ {
					localList[i] = false
				}
				commitEvent := &common.CommitEvent{
					Block:     block,
					LocalList: localList,
				}
				s.BlockC <- commitEvent
			}
		}
	}
}

func (s *RBFTAdaptor) checkLocalState(curHeight uint64, checkpoints []*consensus.SignedCheckpoint, change ...*consensus.EpochChange) error {
	// check epoch
	if change != nil {
		lowEpoch := change[0]
		if s.EpochInfo.Epoch > lowEpoch.Checkpoint.Epoch() {
			return fmt.Errorf("target epoch is too low, current epoch is %d, "+
				"but the epoch which we need update is %d", s.EpochInfo.Epoch, lowEpoch.Checkpoint.Epoch())
		}
		if curHeight > lowEpoch.Checkpoint.Height() {
			return fmt.Errorf("target epoch height is too low, current height is %d, "+
				"but the lowest epoch cahnge height which we need update is %d", curHeight, lowEpoch.Checkpoint.Height())
		}
	}

	// check LocalLatestCheckpoint
	if curHeight >= checkpoints[0].Height() {
		localBlock, err := s.getBlockFunc(checkpoints[0].Height())
		if err != nil {
			return fmt.Errorf("get local block[height:%d] from ledger failed: %w", checkpoints[0].Height(), err)
		}
		if localBlock.BlockHash.String() != checkpoints[0].Checkpoint.Digest() {
			return fmt.Errorf("local block[height:%d, hash:%s] is inconsistent with remote checkpoint[hash:%s]",
				checkpoints[0].Height(), localBlock.BlockHash.String(), checkpoints[0].Checkpoint.Digest())
		}
	}

	return nil
}

func (s *RBFTAdaptor) SendFilterEvent(informType rbfttypes.InformType, message ...any) {
	// TODO: add implement
}
