package simplelog

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
	defaultVerbosityLevel = LvlInfo
)

func AddFlags() {
	flag.IntVar(&defaultVerbosityLevel, "v", LvlInfo, "logging verbosity level")
}

type SimpleLogger struct {
	prefix         string
	log            *log.Logger
	verbosityLevel *int
}

func NewLogger(prefix string) *SimpleLogger {
	return NewLoggerWithVerbosityLevel(prefix, &defaultVerbosityLevel)
}

func NewLoggerWithVerbosityLevel(prefix string, verboseLevel *int) *SimpleLogger {
	return &SimpleLogger{prefix, log.New(os.Stderr, "", log.LstdFlags), verboseLevel}
}

func (l *SimpleLogger) Panicf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	l.log.Print("PANIC - ", l.prefix, " - ", msg)
	panic(msg)
}

func (l *SimpleLogger) Fatalf(format string, v ...interface{}) {
	l.log.Print("FATAL - ", l.prefix, " - ", fmt.Sprintf(format, v...))
	os.Exit(1)
}

func (l *SimpleLogger) Errorf(format string, v ...interface{}) {
	if l.verbosityLevel != nil && *l.verbosityLevel < LvlError {
		return
	}
	l.log.Print("ERROR - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *SimpleLogger) Warn(format string, v ...interface{}) {
	if l.verbosityLevel != nil && *l.verbosityLevel < LvlWarn {
		return
	}
	l.log.Print("WARN  - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *SimpleLogger) Info(format string, v ...interface{}) {
	if l.verbosityLevel != nil && *l.verbosityLevel < LvlInfo {
		return
	}
	l.log.Print("INFO  - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *SimpleLogger) IsDebugEnabled() bool {
	return l.verbosityLevel != nil && *l.verbosityLevel >= LvlDebug
}

func (l *SimpleLogger) Debug(format string, v ...interface{}) {
	if l.verbosityLevel == nil || *l.verbosityLevel < LvlDebug {
		return
	}
	l.log.Print("DEBUG - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}

func (l *SimpleLogger) IsTraceEnabled() bool {
	return l.verbosityLevel != nil && *l.verbosityLevel >= LvlTrace
}

func (l *SimpleLogger) Trace(format string, v ...interface{}) {
	if l.verbosityLevel == nil || *l.verbosityLevel < LvlTrace {
		return
	}
	l.log.Print("TRACE - ", l.prefix, " - ", fmt.Sprintf(format, v...))
}
