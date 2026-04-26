package server

import (
	"time"

	pb "kubeshipper/helmd/gen"
)

type stream interface {
	Send(*pb.Event) error
}

func newEmitter(s stream) func(phase, msg string) {
	return func(phase, msg string) {
		_ = s.Send(&pb.Event{
			Phase:   phase,
			Message: msg,
			Ts:      time.Now().UnixMilli(),
		})
	}
}

func emitError(s stream, msg string) error {
	_ = s.Send(&pb.Event{
		Phase: "error",
		Error: msg,
		Ts:    time.Now().UnixMilli(),
	})
	return nil
}
