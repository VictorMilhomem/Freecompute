package actors

import (
	"fmt"

	"github.com/VictorMilhomem/freecompute/internal/sandbox"
)

type AllowlistPolicy struct {
	allowAny bool
	allowed  map[sandbox.ImageRef]struct{}
}

func NewAllowlistPolicy(allowImages []string, allowAny bool) (*AllowlistPolicy, error) {
	allowed := make(map[sandbox.ImageRef]struct{}, len(allowImages))
	for _, raw := range allowImages {
		ref, err := sandbox.ParseImageRef(raw)
		if err != nil {
			return nil, err
		}
		allowed[ref] = struct{}{}
	}
	return &AllowlistPolicy{
		allowAny: allowAny,
		allowed:  allowed,
	}, nil
}

func (p *AllowlistPolicy) Allow(ref sandbox.ImageRef) error {
	if _, err := sandbox.ParseImageRef(ref.String()); err != nil {
		return err
	}
	if p != nil && p.allowAny {
		return nil
	}
	if p != nil {
		if _, ok := p.allowed[ref]; ok {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", sandbox.ErrImageNotAllowed, ref)
}
