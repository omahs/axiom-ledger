package rbft

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	rbft "github.com/axiomesh/axiom-bft"
	"github.com/axiomesh/axiom-bft/common/consensus"
	"github.com/axiomesh/axiom-kit/log"
	"github.com/axiomesh/axiom-kit/types"
	"github.com/axiomesh/axiom-ledger/internal/consensus/precheck"
	"github.com/axiomesh/axiom-ledger/internal/consensus/precheck/mock_precheck"
	"github.com/axiomesh/axiom-ledger/internal/consensus/rbft/adaptor"
	"github.com/axiomesh/axiom-ledger/internal/consensus/rbft/testutil"
	"github.com/axiomesh/axiom-ledger/internal/consensus/txcache"
	"github.com/axiomesh/axiom-ledger/internal/storagemgr"
	"github.com/axiomesh/axiom-ledger/pkg/repo"
)

var validTxsCh = make(chan *precheck.ValidTxs, 1024)

func MockMinNode(ctrl *gomock.Controller, t *testing.T) *Node {
	err := storagemgr.Initialize(repo.KVStorageTypeLeveldb)
	assert.Nil(t, err)
	mockRbft := rbft.NewMockMinimalNode[types.Transaction, *types.Transaction](ctrl)
	mockRbft.EXPECT().Status().Return(rbft.NodeStatus{
		ID:     uint64(1),
		View:   uint64(1),
		Status: rbft.Normal,
	}).AnyTimes()
	logger := log.NewWithModule("consensus")
	logger.Logger.SetLevel(logrus.DebugLevel)
	consensusConf := testutil.MockConsensusConfig(logger, ctrl, t)

	ctx, cancel := context.WithCancel(context.Background())
	rbftAdaptor, err := adaptor.NewRBFTAdaptor(consensusConf)
	assert.Nil(t, err)
	err = rbftAdaptor.UpdateEpoch()
	assert.Nil(t, err)

	mockPrecheckMgr := mock_precheck.NewMockMinPreCheck(ctrl, validTxsCh)

	rbftConfig, _ := generateRbftConfig(consensusConf)
	node := &Node{
		config:     consensusConf,
		n:          mockRbft,
		stack:      rbftAdaptor,
		logger:     logger,
		network:    consensusConf.Network,
		ctx:        ctx,
		cancel:     cancel,
		txCache:    txcache.NewTxCache(rbftConfig.SetTimeout, uint64(rbftConfig.SetSize), consensusConf.Logger),
		txFeed:     event.Feed{},
		txPreCheck: mockPrecheckMgr,
	}
	return node
}

func TestInit(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	node := MockMinNode(ctrl, t)

	node.config.Config.Rbft.EnableMultiPipes = false
	err := node.initConsensusMsgPipes()
	ast.Nil(err)

	node.config.Config.Rbft.EnableMultiPipes = true
	err = node.initConsensusMsgPipes()
	ast.Nil(err)
}

func TestPrepare(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	node := MockMinNode(ctrl, t)
	mockRbft := rbft.NewMockMinimalNode[types.Transaction, *types.Transaction](ctrl)
	node.n = mockRbft

	err := node.Start()
	ast.Nil(err)

	mockRbft.EXPECT().Status().Return(rbft.NodeStatus{
		Status: rbft.InViewChange,
	}).Times(1)
	err = node.Ready()
	ast.Error(err)

	mockRbft.EXPECT().Status().Return(rbft.NodeStatus{
		Status: rbft.Normal,
	}).AnyTimes()
	err = node.Ready()
	ast.Nil(err)

	txCache := make(map[string]*types.Transaction)
	nonceCache := make(map[string]uint64)
	node.n.(*rbft.MockNode[types.Transaction, *types.Transaction]).EXPECT().Propose(gomock.Any(), gomock.Any()).Do(func(requests []*types.Transaction, local bool) error {
		for _, tx := range requests {
			txCache[tx.RbftGetTxHash()] = tx
			if _, ok := nonceCache[tx.GetFrom().String()]; !ok {
				nonceCache[tx.GetFrom().String()] = tx.GetNonce()
			} else if nonceCache[tx.GetFrom().String()] < tx.GetNonce() {
				nonceCache[tx.GetFrom().String()] = tx.GetNonce()
			}
		}
		return nil
	}).Return(nil).AnyTimes()

	node.n.(*rbft.MockNode[types.Transaction, *types.Transaction]).EXPECT().GetPendingTxCountByAccount(gomock.Any()).DoAndReturn(func(addr string) uint64 {
		if _, ok := nonceCache[addr]; !ok {
			return 0
		}
		return nonceCache[addr] + 1
	}).AnyTimes()

	node.n.(*rbft.MockNode[types.Transaction, *types.Transaction]).EXPECT().GetPendingTxByHash(gomock.Any()).DoAndReturn(func(hash string) *types.Transaction {
		data := txCache[hash]
		return data
	}).AnyTimes()

	node.n.(*rbft.MockNode[types.Transaction, *types.Transaction]).EXPECT().GetTotalPendingTxCount().DoAndReturn(func() uint64 {
		return uint64(len(txCache))
	}).AnyTimes()

	sk, err := crypto.GenerateKey()
	ast.Nil(err)

	toAddr := crypto.PubkeyToAddress(sk.PublicKey)
	tx1, singer, err := types.GenerateTransactionAndSigner(uint64(0), types.NewAddressByStr(toAddr.String()), big.NewInt(0), []byte("hello"))
	ast.Nil(err)

	err = node.Start()
	ast.Nil(err)
	err = node.Prepare(tx1)
	ast.Nil(err)
	tx2, err := types.GenerateTransactionWithSigner(uint64(1), types.NewAddressByStr(toAddr.String()), big.NewInt(0), []byte("hello"), singer)
	ast.Nil(err)
	err = node.Prepare(tx2)
	ast.Nil(err)

	t.Run("GetPendingTxCountByAccount", func(t *testing.T) {
		pendingNonce := node.GetPendingTxCountByAccount(tx1.GetFrom().String())
		ast.Equal(uint64(2), pendingNonce)
	})

	t.Run("GetPendingTxByHash", func(t *testing.T) {
		tx := node.GetPendingTxByHash(tx1.GetHash())
		ast.NotNil(tx.Inner)
		ast.Equal(tx1.GetHash().String(), tx.GetHash().String())
		wrongTx := node.GetPendingTxByHash(types.NewHashByStr("0x123"))
		ast.Nil(wrongTx)
	})

	t.Run("GetTotalPendingTxCount", func(t *testing.T) {
		count := node.GetTotalPendingTxCount()
		ast.Equal(uint64(2), count)
	})

	t.Run("GetLowWatermark", func(t *testing.T) {
		node.n.(*rbft.MockNode[types.Transaction, *types.Transaction]).EXPECT().GetLowWatermark().DoAndReturn(func() uint64 {
			return 1
		}).AnyTimes()
		lowWatermark := node.GetLowWatermark()
		ast.Equal(uint64(1), lowWatermark)
	})
}

func TestStop(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	node := MockMinNode(ctrl, t)

	// test start
	err := node.Start()
	ast.Nil(err)
	ast.Nil(node.checkQuorum())

	now := time.Now()
	node.stack.ReadyC <- &adaptor.Ready{
		Height:    uint64(2),
		Timestamp: now.UnixNano(),
	}
	block := <-node.Commit()
	ast.Equal(uint64(2), block.Block.Height())
	ast.Equal(now.Unix(), block.Block.BlockHeader.Timestamp, "convert nano to second")

	// test stop
	node.Stop()
	time.Sleep(1 * time.Second)
	_, ok := <-node.txCache.CloseC
	ast.Equal(false, ok)
}

func TestReadConfig(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	logger := log.NewWithModule("consensus")
	rbftConf, txpoolConfig := generateRbftConfig(testutil.MockConsensusConfig(logger, ctrl, t))
	rbftConf.Logger.Notice()
	rbftConf.Logger.Noticef("test notice")
	ast.Equal(50, rbftConf.SetSize)
	ast.Equal(uint64(500), txpoolConfig.BatchSize)
	ast.Equal(uint64(50000), txpoolConfig.PoolSize)
	ast.Equal(500*time.Millisecond, rbftConf.BatchTimeout)
	ast.Equal(3*time.Minute, rbftConf.CheckPoolTimeout)
	ast.Equal(5*time.Minute, txpoolConfig.ToleranceTime)
}

func TestStep(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	node := MockMinNode(ctrl, t)
	err := node.Step([]byte("test"))
	ast.NotNil(err)
	msg := &consensus.ConsensusMessage{}
	msgBytes, _ := msg.Marshal()
	err = node.Step(msgBytes)
	ast.Nil(err)
}

func TestReportState(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	node := MockMinNode(ctrl, t)

	block := testutil.ConstructBlock("blockHash", uint64(20))
	node.stack.StateUpdating = true
	node.stack.StateUpdateHeight = 20
	node.ReportState(uint64(10), block.BlockHash, nil, nil)
	ast.Equal(true, node.stack.StateUpdating)

	node.ReportState(uint64(20), block.BlockHash, nil, nil)
	ast.Equal(false, node.stack.StateUpdating)

	node.ReportState(uint64(21), block.BlockHash, nil, nil)
	ast.Equal(false, node.stack.StateUpdating)

	t.Run("ReportStateUpdating with checkpoint", func(t *testing.T) {
		node.stack.StateUpdating = true
		node.stack.StateUpdateHeight = 30
		block30 := testutil.ConstructBlock("blockHash", uint64(30))
		testutil.SetMockBlockLedger(block30, true)
		defer testutil.ResetMockBlockLedger()

		ckp := &consensus.Checkpoint{
			ExecuteState: &consensus.Checkpoint_ExecuteState{
				Height: 30,
				Digest: block30.BlockHash.String(),
			},
		}
		node.ReportState(uint64(30), block.BlockHash, nil, ckp)
		ast.Equal(false, node.stack.StateUpdating)
	})
}

func TestQuorum(t *testing.T) {
	ast := assert.New(t)
	ctrl := gomock.NewController(t)
	node := MockMinNode(ctrl, t)
	node.stack.EpochInfo.ValidatorSet = []*rbft.NodeInfo{}
	node.stack.EpochInfo.ValidatorSet = append(node.stack.EpochInfo.ValidatorSet, &rbft.NodeInfo{ID: 1})
	node.stack.EpochInfo.ValidatorSet = append(node.stack.EpochInfo.ValidatorSet, &rbft.NodeInfo{ID: 2})
	node.stack.EpochInfo.ValidatorSet = append(node.stack.EpochInfo.ValidatorSet, &rbft.NodeInfo{ID: 3})
	node.stack.EpochInfo.ValidatorSet = append(node.stack.EpochInfo.ValidatorSet, &rbft.NodeInfo{ID: 4})

	// N = 3f + 1, f=1
	quorum := node.Quorum()
	ast.Equal(uint64(3), quorum)

	node.stack.EpochInfo.ValidatorSet = append(node.stack.EpochInfo.ValidatorSet, &rbft.NodeInfo{ID: 5})
	// N = 3f + 2, f=1
	quorum = node.Quorum()
	ast.Equal(uint64(4), quorum)

	node.stack.EpochInfo.ValidatorSet = append(node.stack.EpochInfo.ValidatorSet, &rbft.NodeInfo{ID: 6})
	// N = 3f + 3, f=1
	quorum = node.Quorum()
	ast.Equal(uint64(4), quorum)
}

func TestStatus2String(t *testing.T) {
	ast := assert.New(t)

	assertMapping := map[rbft.StatusType]string{
		rbft.Normal: "Normal",

		rbft.InConfChange:      "system is in conf change",
		rbft.InViewChange:      "system is in view change",
		rbft.InRecovery:        "system is in recovery",
		rbft.StateTransferring: "system is in state update",
		rbft.PoolFull:          "system is too busy",
		rbft.Pending:           "system is in pending state",
		rbft.Stopped:           "system is stopped",
		1000:                   "Unknown status: 1000",
	}

	for status, assertStatusStr := range assertMapping {
		statusStr := status2String(status)
		ast.Equal(assertStatusStr, statusStr)
	}
}
