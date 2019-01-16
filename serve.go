// Minimum Go Version
//
// We require Go 1.11 in order to support deadlines:
// This package uses two *os.File (stdin, stdout) to implement a net.Conn interface.
// As such, we need to support deadlines, and Go 1.10 introduced
// deadlines for certain types of *os.File (https://golang.org/doc/go1.10#os).
// However, apart from OS/FS constraints, deadlines on *os.File only work if
// the underlying FD can be added to Go's event pool mechanism, which generally
// requires the FD to be non-blocking at the time the *os.File is created.
// This is the case for os.Open and so on, but not for os.Std{in,out,err},
// which are created using os.NewFile from the existing FDs 0, 1 and 2.
// Those are blocking by default, and Go does not change them to non-blocking
// because the parent process might be a terminal and not expect the tty to
// change to non-blocking mode after the Go process exits.
//
// Hence, in Go 1.10, deadline operations on os.Std{in,out,err} fail with
// os.ErrDeadline.
// Go 1.11 improves the behavior of os.NewFile such that iff the FD passed to
// os.NewFile is already non-blocking, the *os.File / FD is part of the event
// loop, which makes deadlines work iff the OS supports it at all (this is the
// case for stdin, stdout, stderr, which are pipes in our specific use case).
// See https://github.com/golang/go/issues/24842#issuecomment-381268558
//
// How do we deal with this issue in package netssh?
// We require Go 1.11 via build tag to get the os.NewFile behavior described
// above and set os.Std{in,out} to non-blocking before sending it over the unix
// control socket.
// See usage of github.com/ftrvxmtrx/fd in functions Proxy and Listener.Accept.
package netssh

// better build errors than a build tag...
import _ "github.com/theckman/goconstraint/go1.11/gte"

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"time"

	"github.com/ftrvxmtrx/fd"
	"golang.org/x/sys/unix"
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

	// See comment at top of file
	if err := unix.SetNonblock(int(os.Stdin.Fd()), true); err != nil {
		log.Printf("error setting stdin to nonblocking mode: %s", err)
		return err
	}
	if err := unix.SetNonblock(int(os.Stdout.Fd()), true); err != nil {
		log.Printf("error setting stdout to nonblocking mode: %s", err)
		return err
	}

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

// Read implements io.Reader.
// It returns *IOError for any non-nil error that is != io.EOF.
func (f *ServeConn) Read(p []byte) (n int, err error) {
	n, err = f.stdin.Read(p)
	if err != nil && err != io.EOF {
		err = &IOError{err}
	}
	return n, err
}

// Write implements io.Writer.
// It returns *IOError for any error != nil.
func (f *ServeConn) Write(p []byte) (n int, err error) {
	n, err = f.stdout.Write(p)
	if err != nil {
		err = &IOError{err}
	}
	return n, err
}

func (f *ServeConn) Close() (err error) {
	f.stdin.Close()
	f.stdout.Close()
	var buf bytes.Buffer
	buf.Write(([]byte{f.proxyFeedback}))
	io.Copy(f.control, &buf)
	return f.control.Close()
}

func (f *ServeConn) CloseWrite() error {
	return f.stdout.Close()
}

func (f *ServeConn) SetReadDeadline(t time.Time) error {
	return f.stdin.SetReadDeadline(t)
}

func (f *ServeConn) SetWriteDeadline(t time.Time) error {
	return f.stdout.SetReadDeadline(t)
}

func (f *ServeConn) SetDeadline(t time.Time) error {
	// try both...
	werr := f.SetWriteDeadline(t)
	rerr := f.SetReadDeadline(t)
	if werr != nil {
		return werr
	}
	if rerr != nil {
		return rerr
	}
	return nil
}

type serveAddr struct{}

func (serveAddr) Network() string { return go_network }
func (serveAddr) String() string  { return "???" }

func (f *ServeConn) LocalAddr() net.Addr  { return serveAddr{} }
func (f *ServeConn) RemoteAddr() net.Addr { return serveAddr{} }

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
	files, err := fd.Get(unixconn.(*net.UnixConn), 2, []string{"netssh-proxy-stdin", "netssh-proxy-stdout"})
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
