package governance

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/samber/lo"

	"github.com/axiomesh/axiom-kit/types"
	"github.com/axiomesh/axiom-ledger/internal/executor/system/common"
	"github.com/axiomesh/axiom-ledger/internal/ledger"
	"github.com/axiomesh/axiom-ledger/pkg/repo"
	vm "github.com/axiomesh/eth-kit/evm"
)

var (
	ErrCouncilNumber            = errors.New("council members total count can't bigger than candidates count")
	ErrMinCouncilMembersCount   = errors.New("council members count can't less than 4")
	ErrRepeatedAddress          = errors.New("council member address repeated")
	ErrRepeatedName             = errors.New("council member name repeated")
	ErrNotFoundCouncilMember    = errors.New("council member is not found")
	ErrCouncilExtraArgs         = errors.New("unmarshal council extra arguments error")
	ErrNotFoundCouncilProposal  = errors.New("council proposal not found for the id")
	ErrExistNotFinishedProposal = errors.New("exist not finished proposal, must finished all proposal then propose council proposal")
	ErrDeadlineBlockNumber      = errors.New("can't vote, proposal is out of deadline block number")
)

const (
	// CouncilProposalKey is key for CouncilProposal storage
	CouncilProposalKey = "councilProposalKey"

	// CouncilKey is key for council storage
	CouncilKey = "councilKey"

	// MinCouncilMembersCount is min council members count
	MinCouncilMembersCount = 4

	// TODO: set used gas
	// CouncilProposalGas is used gas for council proposal
	CouncilProposalGas uint64 = 30000

	// CouncilVoteGas is used gas for council vote
	CouncilVoteGas uint64 = 21600
)

// CouncilExtraArgs is council proposal extra arguments
type CouncilExtraArgs struct {
	Candidates []*CouncilMember
}

// CouncilProposalArgs is council proposal arguments
type CouncilProposalArgs struct {
	BaseProposalArgs
	CouncilExtraArgs
}

// CouncilProposal is storage of council proposal
type CouncilProposal struct {
	BaseProposal
	Candidates []*CouncilMember
}

// Council is storage of council
type Council struct {
	Members []*CouncilMember
}

type CouncilMember struct {
	Address string
	Weight  uint64
	Name    string
}

// CouncilVoteArgs is council vote arguments
type CouncilVoteArgs struct {
	BaseVoteArgs
}

var _ common.SystemContract = (*CouncilManager)(nil)

type CouncilManager struct {
	gov *Governance

	account         ledger.IAccount
	stateLedger     ledger.StateLedger
	currentLog      *common.Log
	proposalID      *ProposalID
	addr2NameSystem *Addr2NameSystem
}

func NewCouncilManager(cfg *common.SystemContractConfig) *CouncilManager {
	gov, err := NewGov([]ProposalType{CouncilElect}, cfg.Logger)
	if err != nil {
		panic(err)
	}

	return &CouncilManager{
		gov: gov,
	}
}

func (cm *CouncilManager) Reset(lastHeight uint64, stateLedger ledger.StateLedger) {
	addr := types.NewAddressByStr(common.CouncilManagerContractAddr)
	cm.account = stateLedger.GetOrCreateAccount(addr)
	cm.stateLedger = stateLedger
	cm.currentLog = &common.Log{
		Address: addr,
	}
	cm.proposalID = NewProposalID(stateLedger)
	cm.addr2NameSystem = NewAddr2NameSystem(stateLedger)

	// check and update
	cm.checkAndUpdateState(lastHeight)
}

func (cm *CouncilManager) Run(msg *vm.Message) (result *vm.ExecutionResult, err error) {
	defer cm.gov.SaveLog(cm.stateLedger, cm.currentLog)

	// parse method and arguments from msg payload
	args, err := cm.gov.GetArgs(msg)
	if err != nil {
		return nil, err
	}

	switch v := args.(type) {
	case *ProposalArgs:
		councilArgs := &CouncilProposalArgs{
			BaseProposalArgs: v.BaseProposalArgs,
		}

		extraArgs := &CouncilExtraArgs{}
		if err = json.Unmarshal(v.Extra, extraArgs); err != nil {
			return nil, ErrCouncilExtraArgs
		}

		councilArgs.CouncilExtraArgs = *extraArgs

		result, err = cm.propose(msg.From, councilArgs)
	case *VoteArgs:
		voteArgs := &CouncilVoteArgs{
			BaseVoteArgs: v.BaseVoteArgs,
		}

		result, err = cm.vote(msg.From, voteArgs)
	default:
		return nil, errors.New("unknown proposal args")
	}

	return result, err
}

func (cm *CouncilManager) propose(addr ethcommon.Address, args *CouncilProposalArgs) (*vm.ExecutionResult, error) {
	cm.gov.logger.Debugf("Propose council election, addr: %s, args: %+v", addr.String(), args)
	for i, candidate := range args.Candidates {
		cm.gov.logger.Debugf("candidate %d: %+v", i, *candidate)
	}
	baseProposal, err := cm.gov.Propose(&addr, ProposalType(args.ProposalType), args.Title, args.Desc, args.BlockNumber)
	if err != nil {
		return nil, err
	}

	// check proposal council member num
	if len(args.Candidates) < MinCouncilMembersCount {
		return nil, ErrMinCouncilMembersCount
	}

	// check proposal candidates has repeated address
	if len(lo.Uniq[string](lo.Map[*CouncilMember, string](args.Candidates, func(item *CouncilMember, index int) string {
		return item.Address
	}))) != len(args.Candidates) {
		return nil, ErrRepeatedAddress
	}

	// set proposal id
	proposal := &CouncilProposal{
		BaseProposal: *baseProposal,
	}

	isExist, council := checkInCouncil(cm.account, addr.String())
	if !isExist {
		return nil, ErrNotFoundCouncilMember
	}

	if !checkAddr2Name(args.Candidates) {
		return nil, ErrRepeatedName
	}

	if !cm.checkFinishedAllProposal() {
		return nil, ErrExistNotFinishedProposal
	}

	id, err := cm.proposalID.GetAndAddID()
	if err != nil {
		return nil, err
	}
	proposal.ID = id

	proposal.TotalVotes = lo.Sum[uint64](lo.Map[*CouncilMember, uint64](council.Members, func(item *CouncilMember, index int) uint64 {
		return item.Weight
	}))
	proposal.Candidates = args.Candidates

	b, err := cm.saveProposal(proposal)

	// record log
	cm.gov.RecordLog(cm.currentLog, ProposeMethod, &proposal.BaseProposal, b)

	return &vm.ExecutionResult{
		UsedGas:    CouncilProposalGas,
		ReturnData: b,
		Err:        err,
	}, nil
}

// Vote a proposal, return vote status
func (cm *CouncilManager) vote(user ethcommon.Address, voteArgs *CouncilVoteArgs) (*vm.ExecutionResult, error) {
	cm.gov.logger.Debugf("Vote council election, addr: %s, args: %+v", user.String(), voteArgs)
	result := &vm.ExecutionResult{UsedGas: CouncilVoteGas}

	// check user can vote
	isExist, _ := checkInCouncil(cm.account, user.String())
	if !isExist {
		return nil, ErrNotFoundCouncilMember
	}

	// get proposal
	isExist, data := cm.account.GetState([]byte(fmt.Sprintf("%s%d", CouncilProposalKey, voteArgs.ProposalId)))
	if !isExist {
		result.Err = ErrNotFoundCouncilProposal
		return result, nil
	}

	proposal := &CouncilProposal{}
	if err := json.Unmarshal(data, proposal); err != nil {
		return nil, err
	}

	res := VoteResult(voteArgs.VoteResult)
	proposalStatus, err := cm.gov.Vote(&user, &proposal.BaseProposal, res)
	if err != nil {
		return nil, err
	}
	proposal.Status = proposalStatus

	b, err := cm.saveProposal(proposal)
	if err != nil {
		return nil, err
	}

	// if proposal is approved, update the council members
	// TODO: need check block number
	if proposal.Status == Approved {
		council := &Council{
			Members: proposal.Candidates,
		}

		// save council
		cb, err := json.Marshal(council)
		if err != nil {
			return nil, err
		}
		cm.account.SetState([]byte(CouncilKey), cb)

		// set name when proposal approved
		setName(cm.addr2NameSystem, council.Members)

		for i, member := range council.Members {
			cm.gov.logger.Debugf("after vote, now council member %d, %+v", i, *member)
		}
	}

	cm.gov.RecordLog(cm.currentLog, VoteMethod, &proposal.BaseProposal, b)

	// return updated proposal
	result.ReturnData = b
	return result, nil
}

func (cm *CouncilManager) saveProposal(proposal *CouncilProposal) ([]byte, error) {
	b, err := json.Marshal(proposal)
	if err != nil {
		return nil, err
	}
	// save proposal
	cm.account.SetState([]byte(fmt.Sprintf("%s%d", CouncilProposalKey, proposal.ID)), b)

	return b, nil
}

func (cm *CouncilManager) EstimateGas(callArgs *types.CallArgs) (uint64, error) {
	args, err := cm.gov.GetArgs(&vm.Message{Data: *callArgs.Data})
	if err != nil {
		return 0, err
	}

	var gas uint64
	switch args.(type) {
	case *ProposalArgs:
		gas = CouncilProposalGas
	case *VoteArgs:
		gas = CouncilVoteGas
	default:
		return 0, errors.New("unknown proposal args")
	}

	return gas, nil
}

func (cm *CouncilManager) checkAndUpdateState(lastHeight uint64) {
	if isExist, data := cm.account.Query(CouncilProposalKey); isExist {
		for _, proposalData := range data {
			proposal := &CouncilProposal{}
			if err := json.Unmarshal(proposalData, proposal); err != nil {
				cm.gov.logger.Errorf("unmarshal council proposal error: %s", err)
				return
			}

			if proposal.Status == Approved || proposal.Status == Rejected {
				// proposal is finnished, no need update
				continue
			}

			if proposal.BlockNumber != 0 && proposal.BlockNumber <= lastHeight {
				// means proposal is out of deadline,status change to rejected
				proposal.Status = Rejected

				_, err := cm.saveProposal(proposal)
				if err != nil {
					cm.gov.logger.Errorf("save proposal error: %s", err)
				}
			}
		}
	}
}

func InitCouncilMembers(lg ledger.StateLedger, admins []*repo.Admin, initBlance string) error {
	addr2NameSystem := NewAddr2NameSystem(lg)

	balance, _ := new(big.Int).SetString(initBlance, 10)
	council := &Council{}
	for _, admin := range admins {
		lg.SetBalance(types.NewAddressByStr(admin.Address), balance)

		council.Members = append(council.Members, &CouncilMember{
			Address: admin.Address,
			Weight:  admin.Weight,
			Name:    admin.Name,
		})

		// set name
		addr2NameSystem.SetName(admin.Address, admin.Name)
	}

	account := lg.GetOrCreateAccount(types.NewAddressByStr(common.CouncilManagerContractAddr))
	b, err := json.Marshal(council)
	if err != nil {
		return err
	}
	account.SetState([]byte(CouncilKey), b)
	return nil
}

func (cm *CouncilManager) checkFinishedAllProposal() bool {
	if isExist, data := cm.account.Query(CouncilProposalKey); isExist {
		for _, proposalData := range data {
			proposal := &CouncilProposal{}
			if err := json.Unmarshal(proposalData, proposal); err != nil {
				return false
			}

			if proposal.Status == Voting {
				return false
			}
		}
	}

	// TODO: add other proposals status check
	return true
}

func checkInCouncil(account ledger.IAccount, addr string) (bool, *Council) {
	// check council if is exist
	isExist, data := account.GetState([]byte(CouncilKey))
	if !isExist {
		return false, nil
	}
	council := &Council{}
	if err := json.Unmarshal(data, council); err != nil {
		return false, nil
	}

	// check addr if is exist in council
	isExist = common.IsInSlice[string](addr, lo.Map[*CouncilMember, string](council.Members, func(item *CouncilMember, index int) string {
		return item.Address
	}))
	if !isExist {
		return false, nil
	}

	return true, council
}

func checkAddr2Name(members []*CouncilMember) bool {
	// repeated name return false
	if len(lo.Uniq[string](lo.Map[*CouncilMember, string](members, func(item *CouncilMember, index int) string {
		return item.Name
	}))) != len(members) {
		return false
	}

	return true
}

func setName(addr2NameSystem *Addr2NameSystem, members []*CouncilMember) {
	for _, member := range members {
		addr2NameSystem.SetName(member.Address, member.Name)
	}
}
