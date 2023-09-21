package block_sync

import (
	"time"

	"github.com/axiomesh/axiom-kit/types/pb"
)

const (
	maxPendingSize = 1000
	syncMsgPipeID  = "sync_Msg_pipe_v1"

	maxRetryCount       = 5
	waitStateTimeout    = 2 * time.Minute
	requestRetryTimeout = 30 * time.Second
)

type syncMsgType int

const (
	syncMsgType_InvalidBlock syncMsgType = iota
	syncMsgType_TimeoutBlock
)

type invalidMsg struct {
	nodeID string      // 出问题的节点id
	height uint64      // 出问题的区块高度
	typ    syncMsgType // 出问题的类型
}

type wrapperStateResp struct {
	peerID string
	resp   *pb.SyncStateResponse
}
