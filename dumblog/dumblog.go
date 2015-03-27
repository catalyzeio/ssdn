package dumblog

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	verboseFlag *int
)

func AddFlags() {
	verboseFlag = flag.Int("v", 3, "logging verbosity level")
}

type DumbLogger struct {
	prefix string
	log    *log.Logger
}

func NewLogger(prefix string) *DumbLogger {
	return &DumbLogger{prefix, log.New(os.Stderr, "", log.LstdFlags)}
}

func (l *DumbLogger) Panic(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	l.log.Print("PANIC - ", l.prefix, " - ", msg)
	panic(msg)
}

func (l *DumbLogger) Fatal(format string, v ...interface{}) {
	if verboseFlag != nil && *verboseFlag < 1 {
		return
	}
	l.log.Print("FATAL - ", l.prefix, ' ', fmt.Sprintf(format, v...))
	os.Exit(1)
}

func (l *DumbLogger) Warn(format string, v ...interface{}) {
	if verboseFlag != nil && *verboseFlag < 2 {
		return
	}
	l.log.Print("WARN  - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *DumbLogger) Info(format string, v ...interface{}) {
	if verboseFlag != nil && *verboseFlag < 3 {
		return
	}
	l.log.Print("INFO  - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *DumbLogger) IsDebugEnabled() bool {
	return verboseFlag != nil && *verboseFlag >= 4
}

func (l *DumbLogger) Debug(format string, v ...interface{}) {
	if verboseFlag == nil || *verboseFlag < 4 {
		return
	}
	l.log.Print("DEBUG - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *DumbLogger) IsTraceEnabled() bool {
	return verboseFlag != nil && *verboseFlag >= 5
}

func (l *DumbLogger) Trace(format string, v ...interface{}) {
	if verboseFlag == nil || *verboseFlag < 5 {
		return
	}
	l.log.Print("TRACE - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}
