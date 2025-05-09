// Copyright 2012 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package test

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net"
	"runtime"
	"testing"
	"time"
)

type closeWriter interface {
	CloseWrite() error
}

func testPortForward(t *testing.T, n, listenAddr string) {
	server := newServer(t)
	conn := server.Dial(clientConfig())
	defer conn.Close()

	sshListener, err := conn.Listen(n, listenAddr)
	if err != nil {
		if runtime.GOOS == "darwin" && err == io.EOF {
			t.Skipf("skipping test broken on some versions of macOS; see https://go.dev/issue/64959")
		}
		t.Fatal(err)
	}

	errCh := make(chan error, 1)

	go func() {
		defer close(errCh)
		sshConn, err := sshListener.Accept()
		if err != nil {
			errCh <- fmt.Errorf("listen.Accept failed: %v", err)
			return
		}
		defer sshConn.Close()

		_, err = io.Copy(sshConn, sshConn)
		if err != nil && err != io.EOF {
			errCh <- fmt.Errorf("ssh client copy: %v", err)
		}
	}()

	forwardedAddr := sshListener.Addr().String()
	netConn, err := net.Dial(n, forwardedAddr)
	if err != nil {
		t.Fatalf("net dial failed: %v", err)
	}

	readChan := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(netConn)
		readChan <- data
	}()

	// Invent some data.
	data := make([]byte, 100*1000)
	for i := range data {
		data[i] = byte(i % 255)
	}

	var sent []byte
	for len(sent) < 1000*1000 {
		// Send random sized chunks
		m := rand.Intn(len(data))
		n, err := netConn.Write(data[:m])
		if err != nil {
			break
		}
		sent = append(sent, data[:n]...)
	}
	if err := netConn.(closeWriter).CloseWrite(); err != nil {
		t.Errorf("netConn.CloseWrite: %v", err)
	}

	// Check for errors on server goroutine
	err = <-errCh
	if err != nil {
		t.Fatalf("server: %v", err)
	}

	read := <-readChan

	if len(sent) != len(read) {
		t.Fatalf("got %d bytes, want %d", len(read), len(sent))
	}
	if !bytes.Equal(sent, read) {
		t.Fatalf("read back data does not match")
	}

	if err := sshListener.Close(); err != nil {
		t.Fatalf("sshListener.Close: %v", err)
	}

	// Check that the forward disappeared.
	netConn, err = net.Dial(n, forwardedAddr)
	if err == nil {
		netConn.Close()
		t.Errorf("still listening to %s after closing", forwardedAddr)
	}
}

func TestPortForwardTCP(t *testing.T) {
	testPortForward(t, "tcp", "localhost:0")
}

func TestPortForwardUnix(t *testing.T) {
	addr, cleanup := newTempSocket(t)
	defer cleanup()
	testPortForward(t, "unix", addr)
}

func testAcceptClose(t *testing.T, n, listenAddr string) {
	server := newServer(t)
	conn := server.Dial(clientConfig())

	sshListener, err := conn.Listen(n, listenAddr)
	if err != nil {
		if runtime.GOOS == "darwin" && err == io.EOF {
			t.Skipf("skipping test broken on some versions of macOS; see https://go.dev/issue/64959")
		}
		t.Fatal(err)
	}

	quit := make(chan error, 1)
	go func() {
		for {
			c, err := sshListener.Accept()
			if err != nil {
				quit <- err
				break
			}
			c.Close()
		}
	}()
	sshListener.Close()

	select {
	case <-time.After(1 * time.Second):
		t.Errorf("timeout: listener did not close.")
	case err := <-quit:
		t.Logf("quit as expected (error %v)", err)
	}
}

func TestAcceptCloseTCP(t *testing.T) {
	testAcceptClose(t, "tcp", "localhost:0")
}

func TestAcceptCloseUnix(t *testing.T) {
	addr, cleanup := newTempSocket(t)
	defer cleanup()
	testAcceptClose(t, "unix", addr)
}

// Check that listeners exit if the underlying client transport dies.
func testPortForwardConnectionClose(t *testing.T, n, listenAddr string) {
	server := newServer(t)
	client := server.Dial(clientConfig())

	sshListener, err := client.Listen(n, listenAddr)
	if err != nil {
		if runtime.GOOS == "darwin" && err == io.EOF {
			t.Skipf("skipping test broken on some versions of macOS; see https://go.dev/issue/64959")
		}
		t.Fatal(err)
	}

	quit := make(chan error, 1)
	go func() {
		for {
			c, err := sshListener.Accept()
			if err != nil {
				quit <- err
				break
			}
			c.Close()
		}
	}()

	// It would be even nicer if we closed the server side, but it
	// is more involved as the fd for that side is dup()ed.
	server.lastDialConn.Close()

	err = <-quit
	t.Logf("quit as expected (error %v)", err)
}

func TestPortForwardConnectionCloseTCP(t *testing.T) {
	testPortForwardConnectionClose(t, "tcp", "localhost:0")
}

func TestPortForwardConnectionCloseUnix(t *testing.T) {
	addr, cleanup := newTempSocket(t)
	defer cleanup()
	testPortForwardConnectionClose(t, "unix", addr)
}
