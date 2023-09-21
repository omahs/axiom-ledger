package block_sync

import (
	"context"
	"time"

	"github.com/Rican7/retry"
	"github.com/Rican7/retry/strategy"
	rbft "github.com/axiomesh/axiom-bft"
	"github.com/axiomesh/axiom-kit/types"
	"github.com/axiomesh/axiom-kit/types/pb"
	network "github.com/axiomesh/axiom-p2p"
	"github.com/axiomesh/axiom/internal/order"
	"github.com/axiomesh/axiom/internal/peermgr"
	"github.com/sirupsen/logrus"
)

type BaseSync struct {
	syncStatus bool
	blockSync  *BlockSync

	consensus order.Order

	syncMsgPipe             network.Pipe // sync state pipe from p2p
	getBlockFunc            func(height uint64) (*types.Block, error)
	getCurrentEpochInfoFunc func() (*rbft.EpochInfo, error)

	logger logrus.FieldLogger
	ctx    context.Context
	cancel context.CancelFunc
}

func NewBaseSync(peerMgr peermgr.PeerManager) (*BaseSync, error) {
	ctx, cancel := context.WithCancel(context.Background())

	baseSync := &BaseSync{
		ctx:    ctx,
		cancel: cancel,
	}
	syncMsgPipe, err := peerMgr.CreatePipe(ctx, syncMsgPipeID)
	if err != nil {
		return nil, err
	}
	baseSync.syncMsgPipe = syncMsgPipe

	return baseSync, err
}

func (bs *BaseSync) SwitchSyncStatus(status bool) {
	if bs.syncStatus == status {
		bs.logger.Warningf("SwitchSyncStatus: status is already %v", status)
		return
	}
	bs.syncStatus = status
	bs.logger.Info("SwitchSyncStatus: status is ", status)
}

func (bs *BaseSync) Start() error {
	go bs.syncMsgLoop()
	return nil
}

func (bs *BaseSync) Stop() error {
	bs.cancel()
	return nil
}

func (bs *BaseSync) syncMsgLoop() {
	for {
		select {
		case <-bs.ctx.Done():
			return
		default:
			msg := bs.syncMsgPipe.Receive(bs.ctx)
			if msg == nil {
				continue
			}
			bs.handleSyncMsg(msg)
		}
	}
}

func (bs *BaseSync) handleSyncMsg(msg *network.PipeMsg) error {
	syncMsg := &pb.SyncMessage{}
	if err := syncMsg.UnmarshalVT(msg.Data); err != nil {
		bs.logger.WithFields(logrus.Fields{
			"from": msg.From,
			"err":  err,
		}).Error("Unmarshal sync message failed")

		return err
	}

	switch syncMsg.Type {
	case pb.SyncMessage_SYNC_STATE_REQUEST:
		if err := bs.recvSyncStateRequest(msg.From, syncMsg.Data); err != nil {
			return err
		}
	case pb.SyncMessage_SYNC_STATE_RESPONSE:
		if err := bs.recvSyncStateResponse(msg.From, syncMsg.Data); err != nil {
			return err
		}
	}
}

func (bs *BaseSync) recvSyncStateRequest(from string, data []byte) error {
	syncStateRequest := &pb.SyncStateRequest{}
	if err := syncStateRequest.UnmarshalVT(data); err != nil {
		bs.logger.WithFields(logrus.Fields{
			"err": err,
		}).Error("Unmarshal sync state request failed")

		return err
	}
	block, err := bs.getBlockFunc(syncStateRequest.Height)
	if err != nil {
		bs.logger.WithFields(logrus.Fields{
			"err": err,
		}).Error("Get block failed")

		return err
	}

	stateResp := &pb.SyncStateResponse{
		Quorum: bs.consensus.Quorum(),
	}

	// if local block hash mismatch, return latest checkpoint block hash
	if block.BlockHash.String() != syncStateRequest.BlockHash {
		bs.logger.WithFields(logrus.Fields{
			"err": err,
		}).Warn("Block hash mismatch")

		// get latest checkpoint block height and hash
		latestCheckpointH := bs.consensus.GetLowWatermark()
		latestCheckpoint, err := bs.getBlockFunc(latestCheckpointH)
		if err != nil {
			bs.logger.WithFields(logrus.Fields{
				"err": err,
			}).Error("Get latest checkpoint block failed")

			return err
		}

		// set checkpoint state in latest checkpoint block
		stateResp.CheckpointState = &pb.CheckpointState{
			Height: latestCheckpointH,
			Digest: latestCheckpoint.BlockHash.String(),
		}
	} else {
		// set checkpoint state in current block
		stateResp.CheckpointState = &pb.CheckpointState{
			Height: block.Height(),
			Digest: block.BlockHash.String(),
		}
	}

	data, err = stateResp.MarshalVT()
	if err != nil {
		bs.logger.WithFields(logrus.Fields{
			"err": err,
		}).Error("Marshal sync state response failed")

		return err
	}

	// send sync state response
	if err = retry.Retry(func(attempt uint) error {
		err = bs.syncMsgPipe.Send(bs.ctx, from, data)
		if err != nil {
			bs.logger.WithFields(logrus.Fields{
				"err": err,
			}).Error("Send sync state response failed")
			return err
		}
		return nil
	}, strategy.Limit(maxRetryCount), strategy.Wait(200*time.Millisecond)); err != nil {
		bs.logger.WithFields(logrus.Fields{
			"err": err,
		}).Error("Send sync state response failed")
		return err
	}

	return nil
}

func (bs *BaseSync) recvSyncStateResponse(from string, data []byte) error {
	// if not in sync status, ignore sync state response
	if !bs.syncStatus {
		bs.logger.WithFields(logrus.Fields{
			"from": from,
		}).Warn("Not in sync status, ignore sync state response")
		return nil
	}

	syncStateResponse := &pb.SyncStateResponse{}
	if err := syncStateResponse.UnmarshalVT(data); err != nil {
		bs.logger.WithFields(logrus.Fields{
			"err": err,
		}).Error("Unmarshal sync state response failed")

		return err
	}

	if bs.blockSync == nil {
		bs.logger.WithFields(logrus.Fields{
			"sync status": bs.syncStatus,
		}).Error("Block sync is nil")

		return nil
	}

	bs.blockSync.postSyncState(&wrapperStateResp{peerID: from, resp: syncStateResponse})
}
