package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kaitu-io/k2/daemon"
	"github.com/kaitu-io/k2/provider"
)

const (
	healthcheckPingTimeout     = 5 * time.Second
	healthcheckMaxFailures     = 2
	healthcheckFailureFilePath = "/var/run/k2-healthcheck-failures"
)

type healthAction int

const (
	actionNone    healthAction = iota
	actionWait
	actionCleanup
)

// evaluateHealth is a pure function that decides what action to take.
func evaluateHealth(pingOK bool, processAlive bool, consecutiveFailures int, maxFailures int) healthAction {
	if pingOK {
		return actionNone
	}
	if !processAlive {
		return actionNone
	}
	if consecutiveFailures+1 >= maxFailures {
		return actionCleanup
	}
	return actionWait
}

func tryPing(addr string) bool {
	client := &http.Client{Timeout: healthcheckPingTimeout}
	resp, err := client.Get("http://" + addr + "/ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func readPIDFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func readFailureCount(path string) int {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if time.Since(info.ModTime()) > 5*time.Minute {
		os.Remove(path)
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}

func writeFailureCount(path string, n int) {
	os.WriteFile(path, []byte(strconv.Itoa(n)), 0644)
}

func resetFailureCount(path string) {
	os.Remove(path)
}

func cmdHealthcheck() {
	addr := daemon.DefaultAddr
	pidPath := defaultPIDPath()
	failurePath := healthcheckFailureFilePath

	pingOK := tryPing(addr)
	pid := readPIDFile(pidPath)
	processAlive := pid > 0 && isProcessAlive(pid)
	failures := readFailureCount(failurePath)

	action := evaluateHealth(pingOK, processAlive, failures, healthcheckMaxFailures)

	switch action {
	case actionNone:
		resetFailureCount(failurePath)
	case actionWait:
		writeFailureCount(failurePath, failures+1)
		fmt.Fprintf(os.Stderr, "healthcheck: daemon unresponsive (%d/%d)\n",
			failures+1, healthcheckMaxFailures)
	case actionCleanup:
		fmt.Fprintf(os.Stderr, "healthcheck: daemon unresponsive (%d consecutive), forcing cleanup\n",
			failures+1)
		provider.CleanupStaleDNSOverride()
		if pid > 0 {
			killProcess(pid)
		}
		resetFailureCount(failurePath)
	}
}
