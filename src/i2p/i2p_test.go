package i2p

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apimgr/api/src/config"
)

func TestI2PBase64AlphabetRoundTrip(t *testing.T) {
	data := []byte{0xfb, 0xff, 0xfe, 0xfa}
	encoded := samBase64.EncodeToString(data)
	if strings.ContainsAny(encoded, "+/") || !strings.ContainsAny(encoded, "-~") {
		t.Fatalf("unexpected I2P base64 alphabet %q", encoded)
	}
	decoded, err := samBase64.DecodeString(encoded)
	if err != nil || string(decoded) != string(data) {
		t.Fatalf("I2P base64 round trip failed: %v", err)
	}
	standard, err := base64.StdEncoding.DecodeString(strings.NewReplacer("-", "+", "~", "/").Replace(encoded))
	if err != nil || string(standard) != string(data) {
		t.Fatalf("I2P base64 translation failed: %v", err)
	}
}

func TestDestinationLengthAndAddress(t *testing.T) {
	destination := make([]byte, 391)
	destination[386] = 4
	priv := samBase64.EncodeToString(append(destination, make([]byte, 64)...))
	got, err := destinationBytes(priv)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 391 {
		t.Fatalf("destination length = %d, want 391", len(got))
	}
	wantHash := sha256.Sum256(destination)
	want := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(wantHash[:])) + ".b32.i2p"
	if gotAddress := b32Address(got); gotAddress != want {
		t.Fatalf("address = %q, want %q", gotAddress, want)
	}
}

func TestCorruptDestinationRejected(t *testing.T) {
	if _, err := destinationBytes("not-a-key"); err == nil {
		t.Fatal("invalid destination key accepted")
	}
}

func TestCreateEepsiteUsesSeparateSilentForwardSocketAndRetriesID(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close SAM listener: %v", err)
		}
	}()
	commands := make(chan string, 3)
	serverErrors := make(chan error, 2)
	go func() {
		for role := 0; role < 2; role++ {
			conn, err := listener.Accept()
			if err != nil {
				serverErrors <- err
				return
			}
			go func(role int, conn net.Conn) {
				defer func() { _ = conn.Close() }()
				reader := bufio.NewReader(conn)
				hello, err := reader.ReadString('\n')
				if err != nil || !strings.HasPrefix(hello, "HELLO VERSION") {
					serverErrors <- errors.New("missing SAM HELLO")
					return
				}
				_, _ = conn.Write([]byte("HELLO REPLY RESULT=OK VERSION=3.3\n"))
				if role == 0 {
					for _, result := range []string{"DUPLICATED_ID", "OK"} {
						line, err := reader.ReadString('\n')
						if err != nil {
							serverErrors <- err
							return
						}
						commands <- strings.TrimSpace(line)
						_, _ = conn.Write([]byte("SESSION STATUS RESULT=" + result + "\n"))
					}
					_, _ = io.Copy(io.Discard, reader)
					return
				}
				line, err := reader.ReadString('\n')
				if err != nil {
					serverErrors <- err
					return
				}
				commands <- strings.TrimSpace(line)
				_, _ = conn.Write([]byte("STREAM STATUS RESULT=OK\n"))
				_, _ = io.Copy(io.Discard, reader)
			}(role, conn)
		}
	}()

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "site-keys.dat")
	destination := make([]byte, 455)
	destination[386] = 4
	privateKey := samBase64.EncodeToString(destination)
	if err := os.WriteFile(keyPath, []byte(privateKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.I2PConfig{SAMAddress: listener.Addr().String(), BootstrapTimeout: 30, SignatureType: 7}
	building := false
	address, control, forward, err := createEepsite(context.Background(), cfg, cfg.SAMAddress, keyPath, 12345, func() { building = true })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Close() }()
	defer func() { _ = forward.Close() }()
	if !building {
		t.Fatal("building state callback was not called")
	}
	if control.LocalAddr().String() == forward.LocalAddr().String() {
		t.Fatal("control and forward sockets were not separate")
	}
	if !strings.HasSuffix(address, ".b32.i2p") {
		t.Fatalf("unexpected address %q", address)
	}
	first, second, third := <-commands, <-commands, <-commands
	if !strings.Contains(first, "SESSION CREATE") || !strings.Contains(second, "SESSION CREATE") {
		t.Fatalf("missing session commands: %q, %q", first, second)
	}
	if strings.Contains(first, "ID=") && first == second {
		t.Fatal("DUPLICATED_ID retry reused the same ID")
	}
	if !strings.Contains(third, "STREAM FORWARD") || !strings.Contains(third, "SILENT=true") {
		t.Fatalf("forward command missing SILENT=true: %q", third)
	}
	select {
	case err := <-serverErrors:
		t.Fatal(err)
	default:
	}
}

func TestCreateEepsiteRejectsPersistedCorruptKeyWithoutRegeneration(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close SAM listener: %v", err)
		}
	}()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		reader := bufio.NewReader(conn)
		_, _ = reader.ReadString('\n')
		_, _ = conn.Write([]byte("HELLO REPLY RESULT=OK VERSION=3.3\n"))
		_, _ = reader.ReadString('\n')
	}()
	keyPath := filepath.Join(t.TempDir(), "site-keys.dat")
	original := []byte("corrupt persisted key\n")
	if err := os.WriteFile(keyPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.I2PConfig{SAMAddress: listener.Addr().String()}
	_, _, _, err = createEepsite(context.Background(), cfg, cfg.SAMAddress, keyPath, 12345, func() {})
	if !errors.Is(err, errInvalidDestination) && (err == nil || !strings.Contains(err.Error(), "corrupt I2P destination key")) {
		t.Fatalf("expected corrupt key error, got %v", err)
	}
	after, readErr := os.ReadFile(keyPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(original) {
		t.Fatal("corrupt persisted key was replaced")
	}
}
