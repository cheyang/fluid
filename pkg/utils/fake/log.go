package fake

import (
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func NullLogger() logr.Logger {
	return logr.New(log.NullLogSink{})
}
