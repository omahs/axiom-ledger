package peermgr

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"

	network "github.com/axiomesh/axiom-p2p"
)

// TODO: refactor
type OrderMessageEvent struct {
	IsTxsFromRemote bool
	Data            []byte
	Txs             [][]byte
}

type KeyType any

type BasicPeerManager interface {
	// Start
	Start() error

	// Stop
	Stop() error

	PeerID() string

	// CountConnectedPeers counts connected peer numbers
	CountConnectedPeers() uint64

	// Peers return all peers including local peer.
	Peers() []peer.AddrInfo
}

// ony used for mock
type Pipe interface {
	fmt.Stringer
	Send(ctx context.Context, to string, data []byte) error
	Broadcast(ctx context.Context, targets []string, data []byte) error
	Receive(ctx context.Context) *network.PipeMsg
}

//go:generate mockgen -destination mock_peermgr/mock_peermgr.go -package mock_peermgr -source peermgr.go -typed
type PeerManager interface {
	network.PipeManager

	BasicPeerManager
}
