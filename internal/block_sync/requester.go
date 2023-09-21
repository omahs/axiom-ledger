package block_sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/axiomesh/axiom-kit/types"
	network "github.com/axiomesh/axiom-p2p"
)

type requester struct {
	peerID      string
	blockHeight uint64

	quitCh     chan struct{}
	retryCh    chan string      // retry peerID
	invalidCh  chan *invalidMsg // a channel which send invalid msg to syncer
	gotBlockCh chan struct{}

	getBlockMsgPipe network.Pipe
	block           *types.Block

	ctx context.Context
}

func newRequester(ctx context.Context, peerID string, height uint64, invalidCh chan *invalidMsg, pipe network.Pipe) *requester {
	return &requester{
		peerID:          peerID,
		blockHeight:     height,
		invalidCh:       invalidCh,
		quitCh:          make(chan struct{}),
		gotBlockCh:      make(chan struct{}),
		getBlockMsgPipe: pipe,
		ctx:             ctx,
	}
}

func (r *requester) start() {
	go r.requestRoutine()
}

// Responsible for making more requests as necessary
// Returns only when a block is found (e.g. AddBlock() is called)
func (r *requester) requestRoutine() {
OUTER_LOOP:
	for {
		ticker := time.NewTicker(requestRetryTimeout)
		// Send request and wait.
		req := &SyncBlockRequest{height: r.blockHeight}
		data, err := json.Marshal(req)
		if err != nil {
			panic(fmt.Errorf("failed to marshal request: %v", err))
		}
		if err = r.getBlockMsgPipe.Send(r.ctx, r.peerID, data); err != nil {
			r.invalidCh <- &invalidMsg{nodeID: r.peerID, typ: syncMsgType_TimeoutBlock}
			newPeer := <-r.retryCh
			// Retry the new peer
			r.peerID = newPeer
			continue OUTER_LOOP
		}

	WAIT_LOOP:
		for {
			select {
			case <-r.quitCh:
				return
			case <-r.ctx.Done():
				return
			case <-ticker.C:
				// Timeout
				r.invalidCh <- &invalidMsg{nodeID: r.peerID, height: r.blockHeight, typ: syncMsgType_TimeoutBlock}

				newPeer := <-r.retryCh
				r.peerID = newPeer
				// Retry the new peer
				continue OUTER_LOOP
			case <-r.gotBlockCh:
				// We got a block!
				// Continue the for-loop and wait til Quit.
				continue WAIT_LOOP
			}
		}
	}
}

func (r *requester) setBlock(block *types.Block) {
	r.block = block
	r.gotBlockCh <- struct{}{}
}
