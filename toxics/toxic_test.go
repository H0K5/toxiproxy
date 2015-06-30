package toxics_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Shopify/toxiproxy/proxy"
	"github.com/Shopify/toxiproxy/toxics"
	"github.com/Sirupsen/logrus"
	"gopkg.in/tomb.v1"
)

func init() {
	logrus.SetLevel(logrus.FatalLevel)
}

func NewTestProxy(name, upstream string) *proxy.Proxy {
	proxy := proxy.NewProxy()

	proxy.Name = name
	proxy.Listen = "localhost:0"
	proxy.Upstream = upstream

	return proxy
}

func WithEchoServer(t *testing.T, f func(string, chan []byte)) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("Failed to create TCP server", err)
	}

	defer ln.Close()

	response := make(chan []byte, 1)
	tomb := tomb.Tomb{}

	go func() {
		defer tomb.Done()
		src, err := ln.Accept()
		if err != nil {
			select {
			case <-tomb.Dying():
			default:
				t.Fatal("Failed to accept client")
			}
			return
		}

		ln.Close()

		scan := bufio.NewScanner(src)
		if scan.Scan() {
			received := append(scan.Bytes(), '\n')
			response <- received

			src.Write(received)
		}
	}()

	f(ln.Addr().String(), response)

	tomb.Killf("Function body finished")
	ln.Close()
	tomb.Wait()

	close(response)
}

func WithEchoProxy(t *testing.T, f func(proxy net.Conn, response chan []byte, proxyServer *proxy.Proxy)) {
	WithEchoServer(t, func(upstream string, response chan []byte) {
		proxy := NewTestProxy("test", upstream)
		proxy.Start()

		conn, err := net.Dial("tcp", proxy.Listen)
		if err != nil {
			t.Error("Unable to dial TCP server", err)
		}

		f(conn, response, proxy)

		proxy.Stop()
	})
}

func ToxicToJson(t *testing.T, name, typeName string, toxic toxics.Toxic) io.Reader {
	// Hack to add fields to an interface, json will nest otherwise, not embed
	data, err := json.Marshal(toxic)
	if err != nil {
		if t != nil {
			t.Errorf("Failed to marshal toxic for api (1): %v", toxic)
		}
		return nil
	}

	marshal := make(map[string]interface{})
	err = json.Unmarshal(data, &marshal)
	if err != nil {
		if t != nil {
			t.Errorf("Failed to unmarshal toxic (2): %v", toxic)
		}
		return nil
	}
	marshal["name"] = name
	if typeName != "" {
		marshal["type"] = typeName
	}

	data, err = json.Marshal(marshal)
	if err != nil {
		if t != nil {
			t.Errorf("Failed to marshal toxic for api (3): %v", toxic)
		}
		return nil
	}
	return bytes.NewReader(data)
}

func AssertEchoResponse(t *testing.T, client, server net.Conn) {
	msg := []byte("hello world\n")

	_, err := client.Write(msg)
	if err != nil {
		t.Error("Failed writing to TCP server", err)
	}

	scan := bufio.NewScanner(server)
	if !scan.Scan() {
		t.Error("Client unexpectedly closed connection")
	}

	resp := append(scan.Bytes(), '\n')
	if !bytes.Equal(resp, msg) {
		t.Error("Server didn't read correct bytes from client:", string(resp))
	}

	_, err = server.Write(resp)
	if err != nil {
		t.Error("Failed writing to TCP client", err)
	}

	scan = bufio.NewScanner(client)
	if !scan.Scan() {
		t.Error("Server unexpectedly closed connection")
	}

	resp = append(scan.Bytes(), '\n')
	if !bytes.Equal(resp, msg) {
		t.Error("Client didn't read correct bytes from server:", string(resp))
	}
}

func TestPersistentConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("Failed to create TCP server", err)
	}

	defer ln.Close()

	proxy := NewTestProxy("test", ln.Addr().String())
	proxy.Start()
	defer proxy.Stop()

	serverConnRecv := make(chan net.Conn)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Error("Unable to accept TCP connection", err)
		}
		serverConnRecv <- conn
	}()

	conn, err := net.Dial("tcp", proxy.Listen)
	if err != nil {
		t.Error("Unable to dial TCP server", err)
	}

	serverConn := <-serverConnRecv

	proxy.UpToxics.AddToxicJson(ToxicToJson(t, "", "noop", &toxics.NoopToxic{}))
	proxy.DownToxics.AddToxicJson(ToxicToJson(t, "", "noop", &toxics.NoopToxic{}))

	AssertEchoResponse(t, conn, serverConn)

	proxy.UpToxics.ResetToxics()
	proxy.DownToxics.ResetToxics()

	AssertEchoResponse(t, conn, serverConn)

	proxy.UpToxics.ResetToxics()
	proxy.DownToxics.ResetToxics()

	AssertEchoResponse(t, conn, serverConn)

	err = conn.Close()
	if err != nil {
		t.Error("Failed to close TCP connection", err)
	}
}

func TestToxicAddRemove(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("Failed to create TCP server", err)
	}

	defer ln.Close()

	proxy := NewTestProxy("test", ln.Addr().String())
	proxy.Start()
	defer proxy.Stop()

	serverConnRecv := make(chan net.Conn)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Error("Unable to accept TCP connection", err)
		}
		serverConnRecv <- conn
	}()

	conn, err := net.Dial("tcp", proxy.Listen)
	if err != nil {
		t.Error("Unable to dial TCP server", err)
	}

	serverConn := <-serverConnRecv

	running := make(chan struct{})
	go func() {
		enabled := false
		for {
			select {
			case <-running:
				return
			default:
				if enabled {
					proxy.UpToxics.AddToxicJson(ToxicToJson(t, "", "noop", &toxics.NoopToxic{}))
					proxy.DownToxics.RemoveToxic("noop")
				} else {
					proxy.UpToxics.RemoveToxic("noop")
					proxy.DownToxics.AddToxicJson(ToxicToJson(t, "", "noop", &toxics.NoopToxic{}))
				}
				enabled = !enabled
			}
		}
	}()

	for i := 0; i < 100; i++ {
		AssertEchoResponse(t, conn, serverConn)
	}
	close(running)

	err = conn.Close()
	if err != nil {
		t.Error("Failed to close TCP connection", err)
	}
}

func TestProxyLatency(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("Failed to create TCP server", err)
	}

	defer ln.Close()

	proxy := NewTestProxy("test", ln.Addr().String())
	proxy.Start()
	defer proxy.Stop()

	serverConnRecv := make(chan net.Conn)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Error("Unable to accept TCP connection", err)
		}
		serverConnRecv <- conn
	}()

	conn, err := net.Dial("tcp", proxy.Listen)
	if err != nil {
		t.Error("Unable to dial TCP server", err)
	}

	serverConn := <-serverConnRecv

	start := time.Now()
	for i := 0; i < 100; i++ {
		AssertEchoResponse(t, conn, serverConn)
	}
	latency := time.Since(start) / 200
	if latency > 300*time.Microsecond {
		t.Errorf("Average proxy latency > 300µs (%v)", latency)
	} else {
		t.Logf("Average proxy latency: %v", latency)
	}

	err = conn.Close()
	if err != nil {
		t.Error("Failed to close TCP connection", err)
	}
}

func BenchmarkProxyBandwidth(b *testing.B) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		b.Fatal("Failed to create TCP server", err)
	}

	defer ln.Close()

	proxy := NewTestProxy("test", ln.Addr().String())
	proxy.Start()
	defer proxy.Stop()

	buf := []byte(strings.Repeat("hello world ", 1000))

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			b.Error("Unable to accept TCP connection", err)
		}
		buf2 := make([]byte, len(buf))
		for err == nil {
			_, err = conn.Read(buf2)
		}
	}()

	conn, err := net.Dial("tcp", proxy.Listen)
	if err != nil {
		b.Error("Unable to dial TCP server", err)
	}

	b.SetBytes(int64(len(buf)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n, err := conn.Write(buf)
		if err != nil || n != len(buf) {
			b.Errorf("%v, %d == %d", err, n, len(buf))
			break
		}
	}

	err = conn.Close()
	if err != nil {
		b.Error("Failed to close TCP connection", err)
	}
}
