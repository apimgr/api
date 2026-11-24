package tor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cretz/bine/control"
)

var controlPortPattern = regexp.MustCompile(`^PORT=(127\.0\.0\.1:[0-9]{1,5})\n$`)
var onionHostnamePattern = regexp.MustCompile(`^[a-z2-7]{56}\.onion$`)

const maxCapturedTorOutput = 64 * 1024

type processOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *processOutput) Write(data []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(data) >= maxCapturedTorOutput {
		data = data[len(data)-maxCapturedTorOutput:]
		o.buf.Reset()
	}
	if overflow := o.buf.Len() + len(data) - maxCapturedTorOutput; overflow > 0 {
		current := append([]byte(nil), o.buf.Bytes()[overflow:]...)
		o.buf.Reset()
		_, _ = o.buf.Write(current)
	}
	return o.buf.Write(data)
}

func (o *processOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.TrimSpace(o.buf.String())
}

type torProcess struct {
	cmd         *exec.Cmd
	control     *control.Conn
	controlConn net.Conn
	done        chan struct{}
	exitErr     error
	output      *processOutput
}

func spawnTor(binary, torrcPath string) (*torProcess, error) {
	cmd := exec.Command(binary, "-f", torrcPath)
	output := &processOutput{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	process := &torProcess{cmd: cmd, done: make(chan struct{}), output: output}
	go func() {
		process.exitErr = cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

func (p *torProcess) waitControlPort(ctx context.Context, path string, timeout time.Duration) (string, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(path); err == nil {
			if match := controlPortPattern.FindSubmatch(data); match != nil {
				_, port, splitErr := net.SplitHostPort(string(match[1]))
				if splitErr == nil {
					if portNumber, parseErr := strconv.Atoi(port); parseErr == nil && portNumber > 0 && portNumber <= 65535 {
						return string(match[1]), nil
					}
				}
			}
		}
		select {
		case <-p.done:
			return "", fmt.Errorf("tor exited before opening its control port (%v): %s", p.exitErr, p.output.String())
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("control port file not ready after %s: %s", timeout, p.output.String())
		case <-tick.C:
		}
	}
}

func dialControl(ctx context.Context, address string) (*control.Conn, net.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
	if err != nil {
		return nil, nil, err
	}
	ctrl := control.NewConn(textproto.NewConn(conn))
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		_ = ctrl.Close()
		return nil, nil, err
	}
	if err := ctrl.Authenticate(""); err != nil {
		_ = ctrl.Close()
		return nil, nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = ctrl.Close()
		return nil, nil, err
	}
	return ctrl, conn, nil
}

func bootstrapTor(ctx context.Context, ctrl *control.Conn, process *torProcess) (<-chan control.Event, error) {
	events := make(chan control.Event, 32)
	process.setControlDeadline(10 * time.Second)
	if err := ctrl.AddEventListener(events, control.EventCodeStatusClient, control.EventCodeHSDesc); err != nil {
		process.clearControlDeadline()
		return nil, err
	}
	if err := ctrl.SetConf(control.KeyVals("DisableNetwork", "0")...); err != nil {
		process.clearControlDeadline()
		return nil, err
	}
	values, err := ctrl.GetInfo("status/bootstrap-phase")
	process.clearControlDeadline()
	if err != nil {
		return nil, fmt.Errorf("read initial bootstrap status: %w", err)
	}
	if bootstrapComplete(values) {
		return events, nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-process.done:
			return nil, fmt.Errorf("tor exited during bootstrap (%v): %s", process.exitErr, process.output.String())
		case <-ctx.Done():
			return nil, fmt.Errorf("bootstrap timeout: %w", ctx.Err())
		case <-ticker.C:
			// bine delivers control events only while a synchronous command is
			// reading the control socket, so poll bootstrap status to drain them.
			process.setControlDeadline(10 * time.Second)
			values, err := ctrl.GetInfo("status/bootstrap-phase")
			process.clearControlDeadline()
			if err != nil {
				return nil, fmt.Errorf("read bootstrap status: %w", err)
			}
			if bootstrapComplete(values) {
				return events, nil
			}
		case event := <-events:
			status, ok := event.(*control.StatusEvent)
			if ok && status.Action == "BOOTSTRAP" && status.Arguments["PROGRESS"] == "100" {
				return events, nil
			}
		}
	}
}

func bootstrapComplete(values []*control.KeyVal) bool {
	for _, value := range values {
		if strings.Contains(value.Val, "PROGRESS=100") {
			return true
		}
	}
	return false
}

func waitDescriptor(ctx context.Context, events <-chan control.Event, process *torProcess, address string) bool {
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-ticker.C:
			// Reading the control socket also dispatches queued HS_DESC events.
			process.setControlDeadline(10 * time.Second)
			_, err := process.control.GetInfo("status/bootstrap-phase")
			process.clearControlDeadline()
			if err != nil {
				return false
			}
		case event := <-events:
			desc, ok := event.(*control.HSDescEvent)
			if ok && desc.Action == "UPLOADED" && (desc.Address == address || desc.Address == strings.TrimSuffix(address, ".onion")) {
				return true
			}
		}
	}
}

func (p *torProcess) setControlDeadline(timeout time.Duration) {
	if p.controlConn != nil {
		_ = p.controlConn.SetDeadline(time.Now().Add(timeout))
	}
}

func (p *torProcess) clearControlDeadline() {
	if p.controlConn != nil {
		_ = p.controlConn.SetDeadline(time.Time{})
	}
}

func (p *torProcess) close() error {
	var signalErr error
	if p.control != nil {
		if p.controlConn != nil {
			_ = p.controlConn.SetDeadline(time.Now().Add(3 * time.Second))
		}
		signalErr = p.control.Signal("SHUTDOWN")
		_ = p.control.Close()
	} else if p.controlConn != nil {
		_ = p.controlConn.Close()
	}
	if p.cmd == nil || p.cmd.Process == nil {
		return errors.Join(p.exitErr, signalErr)
	}
	select {
	case <-p.done:
		return errors.Join(p.exitErr, signalErr)
	case <-time.After(10 * time.Second):
	}
	if err := p.cmd.Process.Kill(); err != nil {
		return errors.Join(err, signalErr)
	}
	select {
	case <-p.done:
		return errors.Join(p.exitErr, signalErr)
	case <-time.After(5 * time.Second):
		return errors.Join(fmt.Errorf("tor process did not exit after kill"), signalErr)
	}
}

func waitHostname(ctx context.Context, path string, timeout time.Duration) (string, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if contents, err := os.ReadFile(path); err == nil {
			address := strings.TrimSpace(string(contents))
			if onionHostnamePattern.MatchString(address) {
				return address, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", fmt.Errorf("hostname file not ready after %s", timeout)
		case <-tick.C:
		}
	}
}

func controlPortPath(dataDir string) string {
	return filepath.Join(dataDir, "control_port")
}
