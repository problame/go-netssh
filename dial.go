package netssh

import (
	"net"
	"fmt"
	"context"
	"io"
	"bytes"
	"github.com/problame/go-rwccmd"
	"os/exec"
	"syscall"
)

type Endpoint struct {
	Host         string
	User         string
	Port         uint16
	IdentityFile string
	SSHCommand   string
	Options      []string
}

func (e Endpoint) CmdArgs() (cmd string, args []string, env []string) {

	if e.SSHCommand != "" {
		cmd = e.SSHCommand
	} else {
		cmd = "ssh"
	}

	args = make([]string, 0, 2*len(e.Options)+4)
	args = append(args,
		"-p", fmt.Sprintf("%d", e.Port),
		"-T",
		"-i", e.IdentityFile,
		"-o", "BatchMode=yes",
	)
	for _, option := range e.Options {
		args = append(args, "-o", option)
	}
	args = append(args, fmt.Sprintf("%s@%s", e.User, e.Host))

	env = []string{}

	return
}

// FIXME: should conform to net.Conn one day, but deadlines as required by net.Conn are complicated:
// it requires to keep the connection open when the deadline is exceeded, but rwcconn.Cmd does not provide Deadlines
// for good reason, see their docs for details.
type SSHConn struct {
	c *rwccmd.Cmd
}

const go_network string = "SSH"

type addr struct {
	pid int
}

func (a addr) Network() string {
	return go_network
}

func (a addr) String() string {
	return fmt.Sprintf("pid=%d", a.pid)
}

func (conn *SSHConn) LocalAddr() net.Addr {
	return addr{conn.c.Pid()}
}

func (conn *SSHConn) RemoteAddr() net.Addr {
	return addr{conn.c.Pid()}
}

func (conn *SSHConn) Read(p []byte) (int, error) {
	return conn.c.Read(p)
}

func (conn *SSHConn) Write(p []byte) (int, error) {
	return conn.c.Write(p)
}

func (conn *SSHConn) Close() (error) {
	return conn.c.Close()
}

// Use at your own risk...
func (conn *SSHConn) Cmd() *rwccmd.Cmd {
	return conn.c
}

const bannerMessageLen = 31
var messages = make(map[string][]byte)
func mustMessage(str string) []byte {
	if len(str) > bannerMessageLen {
		panic("message length must be smaller than bannerMessageLen")
	}
	if _, ok := messages[str]; ok {
		panic("duplicate message")
	}
	var buf bytes.Buffer
	n, _ := buf.WriteString(str)
	if n != len(str) {
		panic("message must only contain ascii / 8-bit chars")
	}
	buf.Write(bytes.Repeat([]byte{0}, bannerMessageLen-n))
	return buf.Bytes()
}
var banner_msg = mustMessage("SSHCON_HELO")
var proxy_error_msg = mustMessage("SSHCON_PROXY_ERROR")
var begin_msg = mustMessage("SSHCON_BEGIN")

type SSHError struct {
	RWCError error
	WhileActivity string
}

// Error() will try to present a one-line error message unless ssh stderr output is longer than one line
func (e *SSHError) Error() string {

	if e.RWCError == io.EOF {
		// rwccmd returns io.EOF on exit status 0, but we do not expect ssh to do that
		return fmt.Sprintf("ssh exited unexpectedly with exit status 0")
	}

	exitErr, ok := e.RWCError.(*exec.ExitError)
	if !ok {
		return fmt.Sprintf("ssh: %s", e.RWCError)
	}

	ws := exitErr.ProcessState.Sys().(syscall.WaitStatus)
	var wsmsg string
	if ws.Exited() {
		wsmsg = fmt.Sprintf("(exit status %d)", ws.ExitStatus())
	} else {
		wsmsg = fmt.Sprintf("(%s)", ws.Signal())
	}

	haveSSHMessage := len(exitErr.Stderr) > 0
	sshOnelineStderr := false
	if i := bytes.Index(exitErr.Stderr, []byte("\n")); i == len(exitErr.Stderr)-1 {
		sshOnelineStderr = true
	}
	stderr := bytes.TrimSpace(exitErr.Stderr)

	if haveSSHMessage {
		if sshOnelineStderr {
			return fmt.Sprintf("ssh: '%s' %s", stderr, wsmsg) // FIXME proper single-quoting
		} else {
			return fmt.Sprintf("ssh %s\n%s", wsmsg, stderr)
		}
	}

	return fmt.Sprintf("ssh terminated without stderr output %s", wsmsg)

}

type ProtocolError struct {
	What string
}

func (e ProtocolError) Error() string {
	return e.What
}

// Dial connects to the remote endpoint where it expects a command executing Proxy().
// Dial performs a handshake consisting of the exchange of banner messages before returning the connection.
// If the handshake cannot be completed before dialCtx is Done(), the underlying ssh command is killed
// and the dialCtx.Err() returned.
// If the handshake completes, dialCtx's deadline does not affect the returned connection.
//
// Errors returned are either dialCtx.Err(), or intances of ProtocolError or *SSHError
func Dial(dialCtx context.Context, endpoint Endpoint) (*SSHConn , error) {

	sshCmd, sshArgs, sshEnv := endpoint.CmdArgs()
	commandCtx, commandCancel := context.WithCancel(context.Background())
	cmd, err := rwccmd.CommandContext(commandCtx, sshCmd, sshArgs, sshEnv)
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}

	confErrChan := make(chan error)
	go func() {
		defer close(confErrChan)
		var buf bytes.Buffer
		if _, err := io.CopyN(&buf, cmd, int64(len(banner_msg))); err != nil {
			confErrChan <- &SSHError{err, "read banner"}
			return
		}
		resp := buf.Bytes()
		switch {
		case bytes.Equal(resp, banner_msg):
			break
		case bytes.Equal(resp, proxy_error_msg):
			confErrChan <- ProtocolError{"proxy error, check remote configuration"}
			return
		default:
			confErrChan <- ProtocolError{fmt.Sprintf("unknown banner message: %v", resp)}
			return
		}
		buf.Reset()
		buf.Write(begin_msg)
		if _, err := io.Copy(cmd, &buf); err != nil {
			confErrChan <- &SSHError{err, "send begin message"}
			return
		}
	}()

	select {
	case <-dialCtx.Done():

		commandCancel()
		// cancelling will make one of the calls in above goroutine fail,
		// and the goroutine will send the error to confErrChan
		//
		// ignore the error and return the cancellation cause

		// draining always terminates because we know the channel is always closed
		for _ = range confErrChan {}

		return nil, dialCtx.Err()

	case err := <-confErrChan:
		if err != nil {
			commandCancel()
			return nil, err
		}
	}

	return &SSHConn{cmd}, nil
}
