package toxics_test

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Shopify/toxiproxy/proxy"
	"github.com/Shopify/toxiproxy/toxics"
)

func AssertDeltaTime(t *testing.T, message string, actual, expected, delta time.Duration) {
	diff := actual - expected
	if diff < 0 {
		diff *= -1
	}
	if diff > delta {
		t.Errorf("[%s] Time was more than %v off: got %v expected %v", message, delta, actual, expected)
	} else {
		t.Logf("[%s] Time was correct: %v (expected %v)", message, actual, expected)
	}
}

func DoLatencyTest(t *testing.T, upLatency, downLatency *toxics.LatencyToxic) {
	WithEchoProxy(t, func(conn net.Conn, response chan []byte, proxy *proxy.Proxy) {
		if upLatency == nil {
			upLatency = &toxics.LatencyToxic{}
		} else {
			_, err := proxy.UpToxics.AddToxicJson(ToxicToJson(t, "", "latency", upLatency))
			if err != nil {
				t.Error("AddToxicJson returned error:", err)
			}
		}
		if downLatency == nil {
			downLatency = &toxics.LatencyToxic{}
		} else {
			_, err := proxy.DownToxics.AddToxicJson(ToxicToJson(t, "", "latency", downLatency))
			if err != nil {
				t.Error("AddToxicJson returned error:", err)
			}
		}
		t.Logf("Using latency: Up: %dms +/- %dms, Down: %dms +/- %dms", upLatency.Latency, upLatency.Jitter, downLatency.Latency, downLatency.Jitter)

		msg := []byte("hello world " + strings.Repeat("a", 32*1024) + "\n")

		timer := time.Now()
		_, err := conn.Write(msg)
		if err != nil {
			t.Error("Failed writing to TCP server", err)
		}

		resp := <-response
		if !bytes.Equal(resp, msg) {
			t.Error("Server didn't read correct bytes from client:", string(resp))
		}
		AssertDeltaTime(t,
			"Server read",
			time.Since(timer),
			time.Duration(upLatency.Latency)*time.Millisecond,
			time.Duration(upLatency.Jitter+10)*time.Millisecond,
		)
		timer2 := time.Now()

		scan := bufio.NewScanner(conn)
		if scan.Scan() {
			resp = append(scan.Bytes(), '\n')
			if !bytes.Equal(resp, msg) {
				t.Error("Client didn't read correct bytes from server:", string(resp))
			}
		}
		AssertDeltaTime(t,
			"Client read",
			time.Since(timer2),
			time.Duration(downLatency.Latency)*time.Millisecond,
			time.Duration(downLatency.Jitter+10)*time.Millisecond,
		)
		AssertDeltaTime(t,
			"Round trip",
			time.Since(timer),
			time.Duration(upLatency.Latency+downLatency.Latency)*time.Millisecond,
			time.Duration(upLatency.Jitter+downLatency.Jitter+10)*time.Millisecond,
		)

		proxy.UpToxics.RemoveToxic("latency")
		proxy.DownToxics.RemoveToxic("latency")

		err = conn.Close()
		if err != nil {
			t.Error("Failed to close TCP connection", err)
		}
	})
}

func TestUpstreamLatency(t *testing.T) {
	DoLatencyTest(t, &toxics.LatencyToxic{Latency: 100}, nil)
}

func TestDownstreamLatency(t *testing.T) {
	DoLatencyTest(t, nil, &toxics.LatencyToxic{Latency: 100})
}

func TestFullstreamLatencyEven(t *testing.T) {
	DoLatencyTest(t, &toxics.LatencyToxic{Latency: 100}, &toxics.LatencyToxic{Latency: 100})
}

func TestFullstreamLatencyBiasUp(t *testing.T) {
	DoLatencyTest(t, &toxics.LatencyToxic{Latency: 1000}, &toxics.LatencyToxic{Latency: 100})
}

func TestFullstreamLatencyBiasDown(t *testing.T) {
	DoLatencyTest(t, &toxics.LatencyToxic{Latency: 100}, &toxics.LatencyToxic{Latency: 1000})
}

func TestZeroLatency(t *testing.T) {
	DoLatencyTest(t, &toxics.LatencyToxic{Latency: 0}, &toxics.LatencyToxic{Latency: 0})
}

func TestLatencyToxicCloseRace(t *testing.T) {
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("Failed to create TCP server", err)
	}

	defer ln.Close()

	proxy := NewTestProxy("test", ln.Addr().String())
	proxy.Start()
	defer proxy.Stop()

	go func() {
		for {
			_, err := ln.Accept()
			if err != nil {
				return
			}
		}
	}()

	// Check for potential race conditions when interrupting toxics
	for i := 0; i < 1000; i++ {
		proxy.UpToxics.AddToxicJson(ToxicToJson(t, "latency", "", &toxics.LatencyToxic{Latency: 10}))
		conn, err := net.Dial("tcp", proxy.Listen)
		if err != nil {
			t.Error("Unable to dial TCP server", err)
		}
		conn.Write([]byte("hello"))
		conn.Close()
		proxy.UpToxics.RemoveToxic("latency")
	}
}
