package dumblog

import (
	"flag"
	"fmt"
	"log"
	"os"
)

const (
	LvlPanic int = iota // also fatal
	LvlError
	LvlWarn
	LvlInfo
	LvlDebug
	LvlTrace
)

var (
	verboseFlag *int
)

func AddFlags() {
	verboseFlag = flag.Int("v", LvlInfo, "logging verbosity level")
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
	l.log.Print("FATAL - ", l.prefix, ' ', fmt.Sprintf(format, v...))
	os.Exit(1)
}

func (l *DumbLogger) Error(format string, v ...interface{}) {
	if verboseFlag != nil && *verboseFlag < LvlError {
		return
	}
	l.log.Print("ERROR - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *DumbLogger) Warn(format string, v ...interface{}) {
	if verboseFlag != nil && *verboseFlag < LvlWarn {
		return
	}
	l.log.Print("WARN  - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *DumbLogger) Info(format string, v ...interface{}) {
	if verboseFlag != nil && *verboseFlag < LvlInfo {
		return
	}
	l.log.Print("INFO  - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *DumbLogger) IsDebugEnabled() bool {
	return verboseFlag != nil && *verboseFlag >= LvlDebug
}

func (l *DumbLogger) Debug(format string, v ...interface{}) {
	if verboseFlag == nil || *verboseFlag < LvlDebug {
		return
	}
	l.log.Print("DEBUG - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *DumbLogger) IsTraceEnabled() bool {
	return verboseFlag != nil && *verboseFlag >= LvlTrace
}

func (l *DumbLogger) Trace(format string, v ...interface{}) {
	if verboseFlag == nil || *verboseFlag < LvlTrace {
		return
	}
	l.log.Print("TRACE - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}
