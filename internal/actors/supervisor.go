package actors

import (
	"errors"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
)

type SpawnChildReq struct {
	Spec act.SupervisorChildSpec
}

type SpawnChildResp struct {
	Err error
}

type ChildStartedMsg struct {
	Name gen.Atom
	PID  gen.PID
}

type ChildTerminatedMsg struct {
	Name   gen.Atom
	PID    gen.PID
	Reason error
}

type retryAddChildMsg struct {
	Spec act.SupervisorChildSpec
}

type NodeSupervisor struct {
	act.Supervisor
	chain      ChainAPI
	bridgeAtom gen.Atom
	bridgeCfg  BridgeConfig
	dispAtom   gen.Atom
	dispCfg    DispatcherConfig
}

func NewNodeSupervisor(
	chain ChainAPI,
	bridgeAtom gen.Atom,
	bridgeCfg BridgeConfig,
	dispAtom gen.Atom,
	dispCfg DispatcherConfig,
) *NodeSupervisor {
	return &NodeSupervisor{
		chain:      chain,
		bridgeAtom: bridgeAtom,
		bridgeCfg:  bridgeCfg,
		dispAtom:   dispAtom,
		dispCfg:    dispCfg,
	}
}

func (s *NodeSupervisor) Init(args ...any) (act.SupervisorSpec, error) {
	return act.SupervisorSpec{
		Type:                act.SupervisorTypeOneForOne,
		EnableHandleChild:   true,
		DisableAutoShutdown: true,
		Restart: act.SupervisorRestart{
			Strategy:  act.SupervisorStrategyTransient,
			Intensity: 100,
			Period:    10,
		},
		Children: []act.SupervisorChildSpec{
			{
				Name: s.dispAtom,
				Factory: func() gen.ProcessBehavior {
					return NewDispatcher(s.dispCfg)
				},
				Restart: act.SupervisorChildRestart{
					Strategy: act.SupervisorStrategyTransient,
				},
			},
			{
				Name: s.bridgeAtom,
				Factory: func() gen.ProcessBehavior {
					return NewChainBridge(s.chain, s.bridgeCfg)
				},
				Restart: act.SupervisorChildRestart{
					Strategy: act.SupervisorStrategyTransient,
				},
			},
		},
	}, nil
}

func (s *NodeSupervisor) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	switch req := request.(type) {
	case SpawnChildReq:
		err := s.addOrStartChild(req.Spec)
		if errors.Is(err, act.ErrSupervisorStrategyActive) {
			_, _ = s.SendAfter(s.PID(), retryAddChildMsg{Spec: req.Spec}, 5*time.Millisecond)
			return SpawnChildResp{Err: nil}, nil
		}
		return SpawnChildResp{Err: err}, nil
	}
	return nil, nil
}

func (s *NodeSupervisor) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case retryAddChildMsg:
		err := s.addOrStartChild(msg.Spec)
		if errors.Is(err, act.ErrSupervisorStrategyActive) {
			_, _ = s.SendAfter(s.PID(), msg, 5*time.Millisecond)
		}
	}
	return nil
}

func (s *NodeSupervisor) addOrStartChild(spec act.SupervisorChildSpec) error {
	err := s.AddChild(spec)
	if errors.Is(err, act.ErrSupervisorChildDuplicate) {
		err = s.StartChild(spec.Name)
		if errors.Is(err, act.ErrSupervisorChildRunning) {
			return nil
		}
	}
	return err
}

func (s *NodeSupervisor) HandleChildStart(name gen.Atom, pid gen.PID) error {
	if name != s.dispAtom && name != s.bridgeAtom {
		_ = s.Send(s.dispAtom, ChildStartedMsg{Name: name, PID: pid})
	}
	return nil
}

func (s *NodeSupervisor) HandleChildTerminate(name gen.Atom, pid gen.PID, reason error) error {
	if name != s.dispAtom && name != s.bridgeAtom {
		_ = s.Send(s.dispAtom, ChildTerminatedMsg{Name: name, PID: pid, Reason: reason})
	}
	return nil
}
