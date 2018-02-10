package netssh

import (
	"context"
)

type contextKey int

const (
	contextKeyLog contextKey = iota
)

type Logger interface {
	Printf(format string, args ...interface{})
}

type discardLog struct {}
func (d discardLog) Printf(format string, args ...interface{}) {}

func contextLog(ctx context.Context) (log Logger) {
	log, ok := ctx.Value(contextKeyLog).(Logger)
	if !ok {
		log = discardLog{}
	}
	return log
}

func ContextWithLog(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, log)
}