package netssh

import (
	"github.com/ftrvxmtrx/fd"
	"io"
	"context"
	"net"
	"os"
	"bytes"
	"time"
	"errors"
)

// The process calling Proxy must exit with non-zero exit status if it returns err != nil
// and a zero exit status if err == nil.
func Proxy(ctx context.Context, server string) (err error) {

	log := contextLog(ctx)

	trySendProxyError := func() {
		log.Printf("writing proxy error to stdout")
		var buf bytes.Buffer
		buf.Write(proxy_error_msg)
		_, err := io.Copy(os.Stdout, &buf)
		if err != nil {
			log.Printf("error writing proxy error: %s", err)
		}
		os.Stdout.Sync()
	}

	log.Printf("connecting to server")
	conn, err := net.Dial("unix", server)
	if err != nil {
		log.Printf("error: %s", err)
		trySendProxyError()
		return err
	}
	defer conn.Close()

	log.Printf("passing stdin and stdout fds to server")
	err = fd.Put(conn.(*net.UnixConn), os.Stdin, os.Stdout)
	if err != nil {
		log.Printf("error: %s", err)
		trySendProxyError()
		return err
	}

	log.Printf("wait for end of connection")
	var dummy [1]byte
	n, err := conn.Read(dummy[:])
	if err != io.EOF {
		log.Printf("error waiting for exit code: %s", err)
		return err
	}
	if n != len(dummy) || dummy[0] != 0 {
		log.Printf("server indicates abnormal termination")
		return errors.New("server indicates abnormal termination")
	}

	log.Printf("server indicates normal termination")
	return nil
}

type ServeConn struct {
	stdin, stdout *os.File
	control       *net.UnixConn
	dlt 			*time.Timer
	proxyFeedback byte
}

func (f *ServeConn) Read(p []byte) (n int, err error) {
	return f.stdin.Read(p)
}

func (f *ServeConn) Write(p []byte) (n int, err error) {
	return f.stdout.Write(p)
}

func (f *ServeConn) Close() (err error) {
	f.stdin.Close()
	f.stdout.Close()
	var buf bytes.Buffer
	buf.Write(([]byte{f.proxyFeedback}))
	io.Copy(f.control, &buf)
	return f.control.Close()
}

type Listener struct {
	l *net.UnixListener
	log Logger
}

func (l *Listener) SetLog(log Logger) {
	l.log = log
}

func (l *Listener) Accept() (*ServeConn, error) {

	var log Logger = discardLog{}
	if l.log != nil {
		log = l.log
	}

	log.Printf("accepting")
	unixconn, err := l.l.Accept()
	if err != nil {
		return nil, err
	}

	log.Printf("receive stdin and stdout fds")
	files, err := fd.Get(unixconn.(*net.UnixConn), 2, []string{"stdin", "stdout"})
	if err != nil {
		unixconn.Close()
		return nil, err
	}

	log.Printf("buidling fdrwc")
	conn := &ServeConn{files[0], files[1], unixconn.(*net.UnixConn), nil, 0}

	var buf bytes.Buffer
	buf.Write(banner_msg)
	if _, err := io.Copy(conn, &buf); err != nil {
		log.Printf("error sending confirm message: %s", err)
		conn.Close()
		return nil, err
	}
	buf.Reset()
	if _, err := io.CopyN(&buf, conn, int64(len(begin_msg))); err != nil {
		log.Printf("error reading begin message: %s", err)
		conn.Close()
		return nil, err
	}

	return conn, nil

}

func (l *Listener) Close() error {
	return l.l.Close()
}

func (l *Listener) Addr() net.Addr {
	return l.l.Addr()
}

func Listen(unixSockPath string) (*Listener, error) {
	unixlistener, err := net.Listen("unix", unixSockPath)
	if err != nil {
		return nil, err
	}
	return &Listener{unixlistener.(*net.UnixListener), nil}, nil
}
