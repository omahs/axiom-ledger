package contracts

import (
	"encoding/json"
	"fmt"

	"github.com/meshplus/bitxhub-core/boltvm"
	"github.com/meshplus/bitxhub-core/governance"
	ruleMgr "github.com/meshplus/bitxhub-core/rule-mgr"
	"github.com/meshplus/bitxhub-model/constant"
	"github.com/meshplus/bitxhub-model/pb"
	"github.com/meshplus/bitxhub/internal/ledger"
	"github.com/tidwall/gjson"
)

// RuleManager is the contract manage validation rules
type RuleManager struct {
	boltvm.Stub
	ruleMgr.RuleManager
}

// extra: ruleMgr.rule
func (rm *RuleManager) Manage(eventTyp string, proposalResult string, extra []byte) *boltvm.Response {
	specificAddrs := []string{constant.GovernanceContractAddr.Address().String()}
	addrsData, err := json.Marshal(specificAddrs)
	if err != nil {
		return boltvm.Error("marshal specificAddrs error:" + err.Error())
	}
	res := rm.CrossInvoke(constant.RoleContractAddr.String(), "CheckPermission",
		pb.String(string(PermissionSpecific)),
		pb.String(""),
		pb.String(rm.CurrentCaller()),
		pb.Bytes(addrsData))
	if !res.Ok {
		return boltvm.Error("check permission error:" + string(res.Result))
	}

	rm.RuleManager.Persister = rm.Stub
	rule := &ruleMgr.Rule{}
	if err := json.Unmarshal(extra, rule); err != nil {
		return boltvm.Error("unmarshal json error:" + err.Error())
	}

	ok, errData := rm.RuleManager.ChangeStatus(rule.Address, proposalResult, []byte(rule.ChainId))
	if !ok {
		return boltvm.Error(string(errData))
	}

	return boltvm.Success(nil)
}

// BindRule binds the validation rule address with the chain id
func (rm *RuleManager) BindRule(chainId string, ruleAddress string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub

	// 1. check permission
	if err := rm.checkPermission(chainId, PermissionSelf); err != nil {
		return boltvm.Error(err.Error())
	}

	// 2. check appchain
	if res := rm.CrossInvoke(constant.AppchainMgrContractAddr.String(), "IsAvailable", pb.String(chainId)); !res.Ok {
		return boltvm.Error("cross invoke IsAvailable error: " + string(res.Result))
	}

	// 3. check rule
	if err := rm.checkRuleAddress(ruleAddress); err != nil {
		return boltvm.Error(err.Error())
	}

	// 4. pre bind
	if ok, data := rm.RuleManager.BindPre(chainId, ruleAddress); !ok {
		return boltvm.Error("bind prepare error: " + string(data))
	}

	// 5. change status
	if ok, data := rm.RuleManager.ChangeStatus(ruleAddress, string(governance.EventBind), []byte(chainId)); !ok {
		return boltvm.Error(string(data))
	}

	// 6. submit proposal
	return rm.CrossInvoke(constant.GovernanceContractAddr.String(), "SubmitProposal",
		pb.String(rm.Caller()),
		pb.String(string(governance.EventBind)),
		pb.String("des"),
		pb.String(string(RuleMgr)),
		pb.String(ruleAddress),
		pb.Bytes([]byte(ruleAddress)),
	)
}

// UnbindRule unbinds the validation rule address with the chain id
func (rm *RuleManager) UnbindRule(chainId string, ruleAddress string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub

	// 1. check permission
	if err := rm.checkPermission(chainId, PermissionSelf); err != nil {
		return boltvm.Error(err.Error())
	}

	// 2. check appchain
	if res := rm.CrossInvoke(constant.AppchainMgrContractAddr.String(), "IsAvailable", pb.String(chainId)); !res.Ok {
		return boltvm.Error("cross invoke IsAvailable error: " + string(res.Result))
	}

	// 3. change status
	if ok, data := rm.RuleManager.ChangeStatus(ruleAddress, string(governance.EventUnbind), []byte(chainId)); !ok {
		return boltvm.Error(string(data))
	}

	// 4. submit proposal
	return rm.CrossInvoke(constant.GovernanceContractAddr.String(), "SubmitProposal",
		pb.String(rm.Caller()),
		pb.String(string(governance.EventUnbind)),
		pb.String("des"),
		pb.String(string(RuleMgr)),
		pb.String(ruleAddress),
		pb.Bytes([]byte(ruleAddress)),
	)
}

// FreezeRule freezes the validation rule address with the chain id
func (rm *RuleManager) FreezeRule(chainId string, ruleAddress string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub

	// 1. check permission
	if err := rm.checkPermission(chainId, PermissionSelfAdmin); err != nil {
		return boltvm.Error(err.Error())
	}

	// 2. check appchain
	if res := rm.CrossInvoke(constant.AppchainMgrContractAddr.String(), "IsAvailable", pb.String(chainId)); !res.Ok {
		return boltvm.Error("cross invoke IsAvailable error: " + string(res.Result))
	}

	// 3. change status
	if ok, data := rm.RuleManager.ChangeStatus(ruleAddress, string(governance.EventFreeze), []byte(chainId)); !ok {
		return boltvm.Error(string(data))
	}

	// 4. submit proposal
	return rm.CrossInvoke(constant.GovernanceContractAddr.String(), "SubmitProposal",
		pb.String(rm.Caller()),
		pb.String(string(governance.EventFreeze)),
		pb.String("des"),
		pb.String(string(RuleMgr)),
		pb.String(ruleAddress),
		pb.Bytes([]byte(ruleAddress)),
	)
}

// ActivateRule activate the validation rule address with the chain id
func (rm *RuleManager) ActivateRule(chainId string, ruleAddress string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub

	// 1. check permission
	if err := rm.checkPermission(chainId, PermissionSelfAdmin); err != nil {
		return boltvm.Error(err.Error())
	}

	// 2. check appchain
	if res := rm.CrossInvoke(constant.AppchainMgrContractAddr.String(), "IsAvailable", pb.String(chainId)); !res.Ok {
		return boltvm.Error("cross invoke IsAvailable error: " + string(res.Result))
	}

	// 3. change status
	if ok, data := rm.RuleManager.ChangeStatus(ruleAddress, string(governance.EventActivate), []byte(chainId)); !ok {
		return boltvm.Error(string(data))
	}

	// 4. submit proposal
	return rm.CrossInvoke(constant.GovernanceContractAddr.String(), "SubmitProposal",
		pb.String(rm.Caller()),
		pb.String(string(governance.EventActivate)),
		pb.String("des"),
		pb.String(string(RuleMgr)),
		pb.String(ruleAddress),
		pb.Bytes([]byte(ruleAddress)),
	)
}

// LogoutRule logout the validation rule address with the chain id
func (rm *RuleManager) LogoutRule(chainId string, ruleAddress string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub

	// 1. check permission
	if err := rm.checkPermission(chainId, PermissionSelf); err != nil {
		return boltvm.Error(err.Error())
	}

	// 2. check appchain
	if res := rm.CrossInvoke(constant.AppchainMgrContractAddr.String(), "IsAvailable", pb.String(chainId)); !res.Ok {
		return boltvm.Error("cross invoke IsAvailable error: " + string(res.Result))
	}

	// 3. change status
	if ok, data := rm.RuleManager.ChangeStatus(ruleAddress, string(governance.EventLogout), []byte(chainId)); !ok {
		return boltvm.Error(string(data))
	}

	// 4. submit proposal
	return rm.CrossInvoke(constant.GovernanceContractAddr.String(), "SubmitProposal",
		pb.String(rm.Caller()),
		pb.String(string(governance.EventLogout)),
		pb.String("des"),
		pb.String(string(RuleMgr)),
		pb.String(ruleAddress),
		pb.Bytes([]byte(ruleAddress)),
	)
}

// CountAvailableRules counts all available rules (should be 0 or 1)
func (rm *RuleManager) CountAvailableRules(chainId string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub
	return responseWrapper(rm.RuleManager.CountAvailable([]byte(chainId)))
}

// CountRules counts all rules of a chain
func (rm *RuleManager) CountRules(chainId string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub
	return responseWrapper(rm.RuleManager.CountAll([]byte(chainId)))
}

// Rules returns all rules of a chain
func (rm *RuleManager) Rules(chainId string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub
	return responseWrapper(rm.RuleManager.All([]byte(chainId)))
}

// GetRule returns available rule address by appchain id and rule address
func (rm *RuleManager) GetRuleAddress(chainId, chainType string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub
	return responseWrapper(rm.GetAvailableRuleAddress(chainId, chainType))
}

func (rm *RuleManager) IsAvailableRule(chainId, ruleAddress string) *boltvm.Response {
	rm.RuleManager.Persister = rm.Stub
	return responseWrapper(rm.IsAvailable(chainId, ruleAddress))
}

func (rm *RuleManager) checkRuleAddress(addr string) error {
	ok, data := rm.Persister.GetAccount(addr)
	if !ok {
		return fmt.Errorf("get account error: %s", string(data))
	}

	account := &ledger.Account{}
	if err := json.Unmarshal(data, account); err != nil {
		return fmt.Errorf("unmarshal account error: %s", err.Error())
	}

	if account.Code() == nil {
		return fmt.Errorf("the validation rule does not exist")
	}

	return nil
}

func (rm *RuleManager) checkPermission(chainId string, p Permission) error {
	res := rm.CrossInvoke(constant.AppchainMgrContractAddr.String(), "GetAppchain", pb.String(chainId))
	if !res.Ok {
		return fmt.Errorf("cross invoke GetAppchain error: %s", string(res.Result))
	}

	pubKeyStr := gjson.Get(string(res.Result), "public_key").String()
	addr, err := getAddr(pubKeyStr)
	if err != nil {
		return fmt.Errorf("get addr error: %s", err.Error())
	}

	res = rm.CrossInvoke(constant.RoleContractAddr.String(), "CheckPermission",
		pb.String(string(p)),
		pb.String(addr),
		pb.String(rm.CurrentCaller()),
		pb.Bytes(nil))
	if !res.Ok {
		return fmt.Errorf("check permission error: %s", string(res.Result))
	}

	return nil
}
func RuleKey(id string) string {
	return ruleMgr.RULEPREFIX + id
}
