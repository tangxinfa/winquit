package winquit

import (
	"fmt"
	"os"
	"time"

	"github.com/containers/winquit/pkg/winquit/win32"
	"github.com/sirupsen/logrus"
)

func requestQuit(pid int) error {
	threads, err := win32.GetProcThreads(uint32(pid))
	if err != nil {
		return err
	}

	closed := false
	for _, thread := range threads {
		logrus.Debugf("Closing windows on process %v thread %d", pid, thread)
		if win32.CloseThreadWindows(uint32(thread)) {
			closed = true
		}
	}
	if !closed {
		return fmt.Errorf("Process %v no windows closed", pid)
	}

	return nil
}

func quitProcess(pid int, waitNicely time.Duration) error {
	_ = RequestQuit(pid)

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	done := make(chan bool)

	go func() {
		proc.Wait()
		done <- true
	}()

	select {
	case <-done:
		return nil
	case <-time.After(waitNicely):
	}

	return proc.Kill()
}
