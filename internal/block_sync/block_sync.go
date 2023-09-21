package block_sync

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Rican7/retry"
	"github.com/Rican7/retry/strategy"
	"github.com/axiomesh/axiom-bft/common/consensus"
	"github.com/axiomesh/axiom-kit/types"
	"github.com/axiomesh/axiom-kit/types/pb"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

type BlockSync struct {
	peers         []string              // p2p set of latest epoch validatorSet
	curHeight     uint64                // latest block height in ledger
	recvBlockSize uint64                // current chunk had received block size
	curBlockHash  string                // latest block hash in ledger
	targetHeight  uint64                // sync target block height
	numPending    atomic.Int64          // requester number
	requesters    map[uint64]*requester // requester map

	quorumCheckpoint *consensus.Checkpoint    // latest checkpoint from remote
	epochChanges     []*consensus.EpochChange // every epoch change which the node behind
	getBlockFunc     func(height uint64) (*types.Block, error)

	lock            sync.RWMutex
	blockCacheCh    chan []*types.Block    // restore block temporary
	chunkSizes      []uint64               // every chunk max size
	recvStateCh     chan *wrapperStateResp // receive state from remote peer
	quitStateCh     chan struct{}          // quit state channel
	chunkTaskDoneCh chan struct{}          // every chunk task done signal
	syncTaskDoneCh  chan struct{}          // all chunk task done signal

	invalidRequestCh chan *invalidMsg // timeout or invalid of sync Block request

	ctx    context.Context
	cancel context.CancelFunc

	logger logrus.FieldLogger
}

func New(logger logrus.FieldLogger, peers []string, curBlockHash string, curHeight, targetHeight uint64,
	fn func(height uint64) (*types.Block, error), quorumCheckpoint *consensus.Checkpoint, epc ...*consensus.EpochChange) (*BlockSync, error) {

	ctx, cancel := context.WithCancel(context.Background())
	syncer := &BlockSync{
		logger:           logger,
		peers:            peers,
		curHeight:        curHeight,
		curBlockHash:     curBlockHash,
		targetHeight:     targetHeight,
		quorumCheckpoint: quorumCheckpoint,
		epochChanges:     epc,

		invalidRequestCh: make(chan *invalidMsg, maxPendingSize),
		chunkTaskDoneCh:  make(chan struct{}),
		syncTaskDoneCh:   make(chan struct{}),
		recvStateCh:      make(chan *wrapperStateResp, len(peers)),
		getBlockFunc:     fn,

		ctx:    ctx,
		cancel: cancel,
	}

	syncer.syncMsgPipe = syncMsgPipe

	return syncer, nil
}

func (s *BlockSync) requestState() error {
	req := &pb.SyncStateRequest{
		LocalHeight:    s.curHeight,
		LocalBlockHash: s.curBlockHash,
	}
	data, err := req.MarshalVT()
	if err != nil {
		return err
	}

	lo.ForEach(s.peers, func(peer string, index int) {
		if err = retry.Retry(func(attempt uint) error {
			err = s.syncMsgPipe.Send(s.ctx, peer, data)
			if err != nil {
				s.logger.WithFields(logrus.Fields{
					"peer": peer,
					"err":  err,
				}).Warn("Send sync state request failed")
				return err
			}
			return nil
		}, strategy.Limit(5), strategy.Wait(1*time.Second)); err != nil {
			s.logger.WithFields(logrus.Fields{
				"peer": peer,
				"err":  err,
			}).Error("Retry Send sync state request failed")
			return
		}
		s.logger.WithFields(logrus.Fields{
			"peer": peer,
		}).Info("Send sync state request success")
	})

	return nil
}

func (s *BlockSync) listenSyncMsg() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.quitStateCh:
			s.logger.Info("sync state task has done, Quit listen sync state")
			return
		default:
			msg := s.syncMsgPipe.Receive(s.ctx)
			if msg == nil {
				return
			}

			syncMsg := &pb.SyncMessage{}
			if err := syncMsg.UnmarshalVT(msg.Data); err != nil {
				s.logger.WithFields(logrus.Fields{
					"from": msg.From,
					"err":  err,
				}).Error("Unmarshal sync message failed")
				continue
			}

			switch syncMsg.Type {
			case pb.SyncMessage_SYNC_STATE_REQUEST:
				s.recvSyncStateRequest(syncMsg.Data)
			}

			resp := &pb.SyncStateResponse{}
			if err := json.Unmarshal(msg.Data, resp); err != nil {
				s.logger.WithField("err", err).Warn("Unmarshal txs message failed")
				continue
			}
			s.recvStateCh <- &wrapperStateResp{
				peerID: msg.From,
				resp:   resp,
			}
		}
	}
}

func (s *BlockSync) listenSyncBlock() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			msg := s.syncBlockPipe.Receive(s.ctx)
			if msg == nil {
				return
			}

			resp := &SyncBlockResponse{}
			if err := json.Unmarshal(msg.Data, resp); err != nil {
				s.logger.WithField("err", err).Warn("Unmarshal txs message failed")
				continue
			}

			s.addBlock(resp.block)
			if s.chunkTaskDone() {
				s.chunkTaskDoneCh <- struct{}{}
			}
		}
	}
}

func (s *BlockSync) recvSyncStateRequest(data []byte) error {
	syncReq := &pb.SyncStateRequest{}
	if err := syncReq.UnmarshalVT(data); err != nil {
		s.logger.WithField("err", err).Error("Unmarshal sync state request failed")
		return err
	}
	block, err := s.getBlockFunc(syncReq.Height)
	if err != nil {
		s.logger.WithField("err", err).Error("Get block failed")
		return err
	}
	if block.BlockHash.String() != syncReq.BlockHash {
		s.logger.WithFields(logrus.Fields{
			"remoteBlockHash": syncReq.BlockHash,
			"localB lockHash": block.BlockHash.String(),
		}).Warn("Local block hash is not equal to remote block hash")
		checkpointHeight := syncReq.Height - s
		block, err := s.getBlockFunc(syncReq.Height - 1)
	}
}

func (s *BlockSync) verifyChunkCheckpoint(checkpoint *consensus.Checkpoint, lastBlock *types.Block) {
	if checkpoint.Height() != lastBlock.Height() {
		panic("quorum checkpoint height is not equal to current chunk max height")
	}
	if checkpoint.Digest() != lastBlock.BlockHash.String() {
		panic("quorum checkpoint hash is not equal to current hash")
	}
}

func (s *BlockSync) addBlock(block *types.Block) {
	if req, ok := s.requesters[block.Height()]; ok {
		if req.block == nil {
			req.setBlock(block)
			s.recvBlockSize++
		}
	}
}

func (s *BlockSync) chunkTaskDone() bool {
	if len(s.chunkSizes) == 0 {
		return true
	}
	return s.recvBlockSize >= s.chunkSizes[0]
}

func (s *BlockSync) postSyncState(wR *wrapperStateResp) {
	s.recvStateCh <- wR

}
func (s *BlockSync) updateState() {
	diffState := make(map[*pb.SyncStateResponse][]string, 0)
	taskDoneCh := make(chan struct{})
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-taskDoneCh:
			s.logger.Debug("Receive quorum state from peers")
			s.quitListenState()
			return
		case <-time.After(waitStateTimeout):
			panic("waiting for quorum state has timed out")
		case msg := <-s.recvStateCh:
			diffState[msg.resp] = append(diffState[msg.resp], msg.peerID)

			// if quorum state is enough, update quorum state
			if len(diffState[msg.resp]) >= int(msg.resp.Quorum) {
				if len(diffState) > len(s.peers)-int(msg.resp.Quorum) {
					panic("quorum state is not enough")
				}

				if msg.resp.CheckpointState.Height != s.curHeight {
					localBlock, err := s.getBlockFunc(msg.resp.CheckpointState.Height)
					if err != nil {
						panic("get local block failed")
					}
					if localBlock.Hash().String() != msg.resp.CheckpointState.Digest {
						panic("local block hash is not equal to quorum state")
					}

					// it means we need rollback to quorum state
					s.curHeight = msg.resp.CheckpointState.Height - 1
				}

				var chunkSize []uint64
				if s.epochChanges != nil {
					chunkSize = make([]uint64, len(s.epochChanges)+1)
					start := s.curHeight
					lo.ForEach(s.epochChanges, func(epoch *consensus.EpochChange, index int) {
						chunkSize[index] = epoch.GetCheckpoint().Height() - start
						start = epoch.GetCheckpoint().Height()
					})

					// handle last chunk
					chunkSize[len(s.epochChanges)] = s.targetHeight - start
				} else {
					// if have no epoch, just one chunkSize
					chunkSize = append(chunkSize, s.targetHeight-s.curHeight)
				}
				s.chunkSizes = chunkSize

				// remove peers which not in quorum state
				delete(diffState, msg.resp)
				if len(diffState) != 0 {
					wrongPeers := lo.Values(diffState)
					lo.ForEach(lo.Flatten(wrongPeers), func(peer string, _ int) {
						if empty := s.removePeer(peer); empty {
							panic("available peer is empty")
						}
					})
				}

				// todo: In cases of network latency, there is an small probability that not reaching a quorum of same state.
				// For example, if the validator set is 4, and the quorum is 3:
				// 1. if the current node is forked,
				// 2. validator need send state which obtaining low watermark height block,
				// 3. validator have different low watermark height block due to network latency,
				// 4. it can lead to state inconsistency, and the node will be stuck in the state sync process.
				taskDoneCh <- struct{}{}
			}
		}
	}
}

func (s *BlockSync) Start() {
	s.logger.WithFields(logrus.Fields{
		"target":      s.targetHeight,
		"current":     s.curHeight,
		"epochChange": s.epochChanges,
	}).Info("Block sync start")

	// start sync state task
	go s.listenSyncState()
	s.updateState()

	// start sync block task
	go s.listenSyncBlock()

	// start sync block request task
	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case msg := <-s.invalidRequestCh:
				s.handleInvalidRequest(msg)

			case <-s.chunkTaskDoneCh:
				s.logger.WithFields(logrus.Fields{
					"start":  s.curHeight,
					"target": s.curHeight + s.chunkSizes[0],
				}).Info("chunk task has done")

				s.updateStatus()

				if s.curHeight == s.targetHeight {
					s.syncTaskDoneCh <- struct{}{}
					s.logger.WithFields(logrus.Fields{
						"target": s.targetHeight,
					}).Info("Block sync done")
				}
				return
			default:
				s.makeRequesters()
			}
		}
	}()
}

func (s *BlockSync) makeRequesters() {
	height := s.curHeight + uint64(len(s.requesters))
	if height > s.curChunkMaxHeight() {
		return
	}
	peerID := s.pickPeer(height)
	request := newRequester(s.ctx, peerID, height, s.invalidRequestCh, s.syncBlockPipe)
	s.requesters[height] = request
	request.start()
}

func (s *BlockSync) curChunkMaxHeight() uint64 {
	if len(s.chunkSizes) == 0 {
		return s.targetHeight
	}
	return s.curHeight + s.chunkSizes[0]
}

func (s *BlockSync) updateStatus() {
	blockCache := make([]*types.Block, s.recvBlockSize)
	for height, r := range s.requesters {
		if r.block == nil {
			panic(fmt.Errorf("requester[height:%d] had no block", height))
		}
		blockCache[height-s.curHeight] = r.block
		s.releaseRequester(height)
	}

	s.blockCacheCh <- blockCache

	var checkpoint *consensus.Checkpoint
	if len(s.epochChanges) != 0 {
		checkpoint = s.epochChanges[0].GetCheckpoint().Checkpoint
	} else {
		checkpoint = s.quorumCheckpoint
	}

	// if checkpoint is not equal to last block, it means we sync wrong block, panic it
	s.verifyChunkCheckpoint(checkpoint, blockCache[len(blockCache)-1])

	s.curHeight += s.chunkSizes[0]
	if len(s.chunkSizes) != 0 {
		s.chunkSizes = s.chunkSizes[1:]
	}
	if len(s.epochChanges) != 0 {
		s.epochChanges = s.epochChanges[1:]
	}
	s.recvBlockSize = 0
}

func (s *BlockSync) quitListenState() {
	s.quitStateCh <- struct{}{}
}

func (s *BlockSync) releaseRequester(height uint64) {
	if s.requesters[height] == nil {
		s.logger.WithFields(logrus.Fields{
			"height": height,
		}).Warn("Release requester Error, requester is nil")
	}
	s.requesters[height].quitCh <- struct{}{}
	delete(s.requesters, height)
}

func (s *BlockSync) handleInvalidRequest(msg *invalidMsg) {
	// retry request
	if s.requesters[msg.height] == nil {
		s.logger.WithFields(logrus.Fields{
			"height": msg.height,
		}).Error("Retry request block Error, requester is nil")
		return
	}
	switch msg.typ {
	case syncMsgType_InvalidBlock:
		s.recvBlockSize--
		newPeer, err := s.pickRandomPeer(msg.nodeID)
		if err != nil {
			panic(err)
		}

		s.requesters[msg.height].retryCh <- newPeer
	case syncMsgType_TimeoutBlock:
		if empty := s.removePeer(msg.nodeID); empty {
			panic("available peer is empty")
		}
		newPeer := s.pickPeer(msg.height)
		s.requesters[msg.height].retryCh <- newPeer
	}
}

func (s *BlockSync) handleBlockResponse(wResp *wrapperBlockResp) {
	curHeight := wResp.resp.block.Height()
	curReq := s.getBlockFromRequester(curHeight)
	if curReq == nil {
		panic(fmt.Errorf("requester[height:%d] is nil", curHeight))
	}
	if curReq.block != nil {
		s.logger.Warningf("receive repeat block[height:%d], ignore it", curHeight)
		return
	}
	var wrong bool
	if prevReq := s.getBlockFromRequester(curHeight - 1); prevReq != nil {
		if prevReq.block != nil {
			if prevReq.block.BlockHash.String() != wResp.resp.block.BlockHeader.ParentHash.String() {
				wrong = true
				s.logger.WithFields(logrus.Fields{
					"height":        curHeight,
					"parentHash":    wResp.resp.block.BlockHeader.ParentHash.String(),
					"prevBlockHash": prevReq.block.BlockHash.String(),
				}).Error("previous Block hash is inconsistent in block parent hash")

				s.handleInconsistentBlock(prevReq)
			}
		}
	}

	if nextReq := s.getBlockFromRequester(curHeight - 1); nextReq != nil {
		if nextReq.block != nil {
			if nextReq.block.BlockHeader.ParentHash.String() != wResp.resp.block.BlockHash.String() {
				wrong = true

				s.logger.WithFields(logrus.Fields{
					"height":              curHeight,
					"curBlockHash":        wResp.resp.block.BlockHash.String(),
					"nextBlockParentHash": nextReq.block.BlockHeader.ParentHash.String(),
				}).Error("Block hash is inconsistent in block parent hash")

				s.handleInconsistentBlock(nextReq)
			}
		}
	}

	if wrong {
		s.handleInconsistentBlock(curReq)
	}
}

func (s *BlockSync) handleInconsistentBlock(req *requester) {
	s.invalidRequestCh <- &invalidMsg{
		nodeID: req.peerID,
		height: req.blockHeight,
		typ:    syncMsgType_InvalidBlock,
	}

	// delete block from cache because it maybe wrong
	req.block = nil
}

func (s *BlockSync) getBlockFromRequester(height uint64) *requester {
	return s.requesters[height]
}

func (s *BlockSync) pickPeer(height uint64) string {
	idx := height % uint64(len(s.peers))
	return s.peers[idx]
}

func (s *BlockSync) pickRandomPeer(exceptPeerId string) (string, error) {
	if exceptPeerId != "" {
		newPeers := lo.Filter(s.peers, func(peer string, _ int) bool {
			return peer != exceptPeerId
		})
		if len(newPeers) == 0 {
			return "", fmt.Errorf("no peer except %s", exceptPeerId)
		}
		return newPeers[rand.Intn(len(newPeers))], nil
	}
	return s.peers[rand.Intn(len(s.peers))], nil
}

func (s *BlockSync) removePeer(peerId string) bool {
	var exist bool
	newPeers := lo.Filter(s.peers, func(peer string, _ int) bool {
		if peer == peerId {
			exist = true
		}
		return peer != peerId
	})
	if !exist {
		s.logger.WithField("peer", peerId).Warn("Remove peer failed, peer not exist")
	}

	s.peers = newPeers
	return len(s.peers) == 0
}

func (s *BlockSync) Stop() {
	s.cancel()
}

func (s *BlockSync) Commit() chan []*types.Block {
	return s.blockCacheCh
}

func (s *BlockSync) SyncDone() chan struct{} {
	return s.syncTaskDoneCh
}
