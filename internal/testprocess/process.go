package testprocess

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func AssertEndpointOK(t *testing.T, service string, done <-chan struct{}, waitErr *error, output *bytes.Buffer, url string) {
	t.Helper()
	client := http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("%s process exited before %s was ready: %v\n%s", service, url, *waitErr, output.String())
		default:
		}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status=%d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s endpoint %s did not become ready: %v\n%s", service, url, lastErr, output.String())
}

func Stop(t *testing.T, cmd *exec.Cmd, done <-chan struct{}, cancel context.CancelFunc) {
	t.Helper()
	select {
	case <-done:
		return
	default:
	}
	cancel()
	if cmd.Process == nil {
		return
	}
	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func GoBinary() string {
	if goroot := runtime.GOROOT(); strings.TrimSpace(goroot) != "" {
		candidate := filepath.Join(goroot, "bin", "go")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "go"
}

func FreeLocalAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on local port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close local listener: %v", err)
	}
	return addr
}

func LocalPort(t *testing.T, addr string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split local addr %s: %v", addr, err)
	}
	return port
}

func EnvWith(overrides map[string]string) []string {
	values := make(map[string]string, len(os.Environ())+len(overrides))
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	for key, value := range overrides {
		values[key] = value
	}
	env := make([]string, 0, len(values))
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}
