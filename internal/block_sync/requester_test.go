package block_sync

import (
	rbft "github.com/axiomesh/axiom-bft"
)

var peerSet = []*rbft.NodeInfo{
	{
		ID:                   1,
		AccountAddress:       "node1",
		P2PNodeID:            "node1",
		ConsensusVotingPower: 1000,
	},
	{
		ID:                   2,
		AccountAddress:       "node2",
		P2PNodeID:            "node2",
		ConsensusVotingPower: 1000,
	},
	{
		ID:                   3,
		AccountAddress:       "node3",
		P2PNodeID:            "node3",
		ConsensusVotingPower: 1000,
	},
	{
		ID:                   4,
		AccountAddress:       "node4",
		P2PNodeID:            "node4",
		ConsensusVotingPower: 1000,
	},
}
