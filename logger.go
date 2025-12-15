package main

import (
	"fmt"
	"github.com/sirupsen/logrus"
	logrus_syslog "github.com/sirupsen/logrus/hooks/syslog"
	"io"
	"log/syslog"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

func setupLogger(debugMode bool, stdOut bool) (l *logrus.Logger, err error) {
	l = logrus.New()
	l.SetReportCaller(true)
	l.SetFormatter(&logrus.TextFormatter{
		ForceColors:               stdOut,
		DisableColors:             !(stdOut),
		ForceQuote:                false,
		DisableQuote:              true,
		EnvironmentOverrideColors: false,
		DisableTimestamp:          false,
		FullTimestamp:             true,
		TimestampFormat:           time.RFC822,
		DisableSorting:            false,
		SortingFunc:               nil,
		DisableLevelTruncation:    false,
		PadLevelText:              true,
		QuoteEmptyFields:          false,
		FieldMap:                  nil,
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := path.Base(f.File)
			return fmt.Sprintf("%s()", f.Function[strings.LastIndex(f.Function, ".")+1:]), fmt.Sprintf(" %s:%d", filename, f.Line)
		},
	})
	if debugMode {
		l.SetLevel(logrus.DebugLevel)
	} else {
		l.SetLevel(logrus.InfoLevel)
	}
	if stdOut {
		l.SetOutput(os.Stdout)
		l.Debugf("Logging to stdout...")
	} else {
		// output to syslog
		if debugMode {
			hook, err = logrus_syslog.NewSyslogHook("", "", syslog.LOG_DEBUG, "")
		} else {
			hook, err = logrus_syslog.NewSyslogHook("", "", syslog.LOG_INFO, "")
		}
		if err != nil {
			l.Error("Unable to connect to local syslog daemon")
		} else {
			l.SetOutput(io.Discard)
			l.AddHook(hook)
			l.Infoln("Logging to syslog...")
		}
	}
	return l, err
}
