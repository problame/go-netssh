package netssh

import (
	"net"
	"fmt"
	"context"
	"io"
	"bytes"
	"github.com/problame/go-rwccmd"
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
		"-q",
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

func Dial(ctx context.Context, endpoint Endpoint) (*SSHConn , error) {

	sshCmd, sshArgs, sshEnv := endpoint.CmdArgs()
	cmd, err := rwccmd.CommandContext(ctx, sshCmd, sshArgs, sshEnv)
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}

	confErrChan := make(chan error)
	go func() {
		var buf bytes.Buffer
		if _, err := io.CopyN(&buf, cmd, int64(len(banner_msg))); err != nil {
			confErrChan <- fmt.Errorf("error reading banner: %s", err)
			return
		}
		resp := buf.Bytes()
		switch {
		case bytes.Equal(resp, banner_msg):
			break
		case bytes.Equal(resp, proxy_error_msg):
			confErrChan <- fmt.Errorf("proxy error, check remote configuration")
			return
		default:
			confErrChan <- fmt.Errorf("unknown banner message: %v", resp)
			return
		}
		buf.Reset()
		buf.Write(begin_msg)
		if _, err := io.Copy(cmd, &buf); err != nil {
			confErrChan <- fmt.Errorf("error sending begin message: %s", err)
			return
		}
		close(confErrChan)
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-confErrChan:
		if err != nil {
			return nil, err
		}
	}

	return &SSHConn{cmd}, err
}
