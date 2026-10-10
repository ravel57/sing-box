package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

var (
	crashLogPath string
	crashLogFile *os.File
	crashLogLock sync.Mutex
)

// setupCrashLog opens the file given by --crash-log and routes Go runtime crashes
// (panics, fatal errors with goroutine dumps) into it. Fatal errors reported through
// writeCrashLog end up there as well. These messages are otherwise printed to stderr only,
// which is lost when sing-box is started by a supervisor (launchd helper, service manager).
func setupCrashLog() {
	if crashLogPath == "" {
		return
	}
	file, err := os.OpenFile(crashLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open crash log %s: %v\n", crashLogPath, err)
		return
	}
	crashLogLock.Lock()
	crashLogFile = file
	crashLogLock.Unlock()
	err = debug.SetCrashOutput(file, debug.CrashOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "set crash output: %v\n", err)
	}
	writeCrashLogLine("sing-box process started, pid=" + fmt.Sprint(os.Getpid()))
}

func writeCrashLogLine(message string) {
	crashLogLock.Lock()
	defer crashLogLock.Unlock()
	if crashLogFile == nil {
		return
	}
	fmt.Fprintf(crashLogFile, "%s CRASHLOG %s\n", time.Now().Format("-0700 2006-01-02 15:04:05"), message)
	_ = crashLogFile.Sync()
}

// writeCrashLog records a fatal error right before the process exits.
func writeCrashLog(err any) {
	writeCrashLogLine(fmt.Sprintf("FATAL: %v", err))
}
