package muxado

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net"
	"sync"
	"testing"
)

func BenchmarkPayload1BStreams1(b *testing.B) {
	testCase(b, 1, 1)
}

func BenchmarkPayload1KBStreams1(b *testing.B) {
	testCase(b, 1024, 1)
}

func BenchmarkPayload1MBStreams1(b *testing.B) {
	testCase(b, 1024*1024, 1)
}

func BenchmarkPayload64MBStreams1(b *testing.B) {
	testCase(b, 64*1024*1024, 1)
}

func BenchmarkPayload1BStreams8(b *testing.B) {
	testCase(b, 1024, 1)
}

func BenchmarkPayload1KBStreams8(b *testing.B) {
	testCase(b, 1024, 8)
}

func BenchmarkPayload1MBStreams8(b *testing.B) {
	testCase(b, 1024*1024, 8)
}

func BenchmarkPayload64MBStreams8(b *testing.B) {
	testCase(b, 64*1024*1024, 8)
}

func BenchmarkPayload1BStreams64(b *testing.B) {
	testCase(b, 1, 64)
}

func BenchmarkPayload1KBStreams64(b *testing.B) {
	testCase(b, 1024, 64)
}

func BenchmarkPayload1MBStreams64(b *testing.B) {
	testCase(b, 1024*1024, 64)
}

func BenchmarkPayload64MBStreams64(b *testing.B) {
	testCase(b, 64*1024*1024, 64)
}

func BenchmarkPayload1KBStreams256(b *testing.B) {
	testCase(b, 1024, 256)
}

func BenchmarkPayload1MBStreams256(b *testing.B) {
	testCase(b, 1024*1024, 256)
}

func BenchmarkPayload64MBStreams256(b *testing.B) {
	testCase(b, 64*1024*1024, 256)
}

func testCase(b *testing.B, payloadSize int64, concurrency int) {
	done := make(chan int)
	c, s := tcpTransport()
	go client(b, c, payloadSize)
	go server(b, s, payloadSize, concurrency, done)
	<-done
}

func server(b *testing.B, sess Session, payloadSize int64, concurrency int, done chan int) {
	go wait(b, sess, "server")

	go func() {
		err, remoteErr, _ := sess.Wait()
		fmt.Printf("session died with err %v, remote: %v\n", err, remoteErr)
	}()

	payloads := make([]*alot, concurrency)
	for i := 0; i < concurrency; i++ {
		payloads[i] = &alot{n: payloadSize}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range payloads {
			p.Reset()
		}
		var wg sync.WaitGroup
		wg.Add(concurrency)
		start := make(chan int)
		for c := 0; c < concurrency; c++ {
			go func(p *alot) {
				<-start
				str, err := sess.OpenStream()
				if err != nil {
					panic(err)
				}
				go func() {
					io.Copy(ioutil.Discard, str)
					wg.Done()
				}()
				n, err := io.Copy(str, p)
				if n != payloadSize {
					b.Errorf("Server failed to send full payload. Got %d, expected %d", n, payloadSize)
				}
				if err != nil {
					panic(err)
				}
				str.CloseWrite()
			}(payloads[c])
		}
		close(start)
		wg.Wait()
	}
	close(done)
}

func client(b *testing.B, sess Session, expectedSize int64) {
	go wait(b, sess, "client")

	for {
		str, err := sess.AcceptStream()
		if err != nil {
			panic(err)
		}

		go func(s Stream) {
			n, err := io.Copy(s, s)
			if err != nil {
				panic(err)
			}
			s.Close()
			if n != expectedSize {
				b.Errorf("stream with wrong size: %d, expected %d", n, expectedSize)
			}
		}(str)
	}
}

func wait(b *testing.B, sess Session, name string) {
	localErr, remoteErr, _ := sess.Wait()
	localCode, _ := GetError(localErr)
	remoteCode, _ := GetError(remoteErr)
	fmt.Printf("'%s' session died with local err %v (code 0x%x), and remote err %v (code 0x%x)\n", localErr, localCode, remoteErr, remoteCode)
	if localCode != NoError || remoteCode != NoError {
		b.Errorf("bad session shutdown")
	}
}

var sourceBuf = bytes.Repeat([]byte("0123456789"), 12800)

type alot struct {
	n     int64
	count int64
}

func (a *alot) Read(p []byte) (int, error) {
	if a.count >= a.n {
		return 0, io.EOF
	}

	remaining := float64(a.n - a.count)
	nbuf := float64(len(p))
	n := int64(math.Min(nbuf, remaining))

	copy(p, sourceBuf[:n])
	a.count += n
	return int(n), nil
}

func (a *alot) Reset() {
	a.count = 0
}

func tcpTransport() (Session, Session) {
	l, port := listener()
	defer l.Close()
	c := make(chan Session)
	s := make(chan Session)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			panic(err)
		}
		s <- Server(conn)
	}()
	go func() {
		conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			panic(err)
		}
		c <- Client(conn)
	}()
	return <-c, <-s
}

type duplexPipe struct {
	*io.PipeReader
	*io.PipeWriter
}

func (dp *duplexPipe) Close() error {
	dp.PipeReader.Close()
	dp.PipeWriter.Close()
	return nil
}

func memTransport() (Session, Session) {
	rd1, wr1 := io.Pipe()
	rd2, wr2 := io.Pipe()
	client := &duplexPipe{rd1, wr2}
	server := &duplexPipe{rd2, wr1}
	return Client(client), Server(server)
}

func listener() (net.Listener, int) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	return l, port
}
