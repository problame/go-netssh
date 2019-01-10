package netssh

import (
	"io/ioutil"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This test asserts the pipes created with exec.Cmd support
// setting deadlines and do in fact support that feature.
func TestExecCmdPipesDeadlineBehavior(t *testing.T) {

	// let's reyly on /bin/sh being present, but it shouldn't really matter
	// since we never call cmd.Start
	newCmd := func() *exec.Cmd {
		return exec.Command("/bin/sh")
	}

	timeout := 1 * time.Second
	expectPipeTimeout := func(t *testing.T, f func() error) {
		pre := time.Now()
		err := f()
		delta := time.Now().Sub(pre)
		t.Logf("%v", delta)
		assert.True(t, delta > 900*time.Millisecond)
		assert.True(t, delta < 1100*time.Millisecond)
		t.Logf("%#v", err)
		pe, ok := err.(*os.PathError)
		require.True(t, ok)
		assert.True(t, pe.Timeout())
	}

	var buf [1 << 15]byte

	t.Run("stdin", func(t *testing.T) {
		t.Parallel()
		cmd := newCmd()
		stdin, err := cmd.StdinPipe()
		require.NoError(t, err)
		dl, ok := stdin.(deadliner)
		require.True(t, ok)
		err = dl.SetWriteDeadline(time.Now().Add(timeout))
		require.NoError(t, err)
		expectPipeTimeout(t, func() error {
			// fill the pipe buffer
			for {
				_, err := stdin.Write(buf[:])
				if err != nil {
					return err
				}
			}
		})
	})

	t.Run("stdout", func(t *testing.T) {
		t.Parallel()
		cmd := newCmd()
		stdout, err := cmd.StdoutPipe()
		require.NoError(t, err)
		dl, ok := stdout.(deadliner)
		require.True(t, ok)
		err = dl.SetReadDeadline(time.Now().Add(timeout))
		require.NoError(t, err)
		expectPipeTimeout(t, func() error {
			_, err := stdout.Read(buf[:])
			return err
		})
	})

	t.Run("stderr", func(t *testing.T) {
		t.Parallel()
		cmd := newCmd()
		stderr, err := cmd.StderrPipe()
		require.NoError(t, err)
		dl, ok := stderr.(deadliner)
		require.True(t, ok)
		err = dl.SetReadDeadline(time.Now().Add(timeout))
		require.NoError(t, err)
		expectPipeTimeout(t, func() error {
			_, err := stderr.Read(buf[:])
			return err
		})
	})

}

// This test verifies the behavior described in serve.go's header comment
func TestStdioIsBlockingByDefault(t *testing.T) {
	// let's hope no one else is mocking around with stdio while this test runs
	err := os.Stdin.SetReadDeadline(time.Now().Add(1 * time.Second))
	assert.Equal(t, err, os.ErrNoDeadline)

	err = os.Stdout.SetWriteDeadline(time.Now().Add(1 * time.Second))
	assert.Equal(t, err, os.ErrNoDeadline)

	err = os.Stderr.SetWriteDeadline(time.Now().Add(1 * time.Second))
	assert.Equal(t, err, os.ErrNoDeadline)
}

// This test verifies the behavior described in serve.go's header comment
// although we don't send the FD through a unix socket, it demonstrates
// the expected behavior of os.NewFile if the fd is set nonblocking.
//
// This test will fail if you compile the test to a binary and run it
// on a readonly FS / system without Go installed.
// More generally, what follows is not particulary stable, but we didn't
// find a better way to assert that the runtime under test (in the child)
// is not affected by the runtime executing the tests.
func TestOsNewFileAndNonblockingWorks(t *testing.T) {
	childSrc := `

	package main

	import "os"
	import "time"
	import "golang.org/x/sys/unix"

	func main() {
		expectDeadlinesToWork := os.Args[1] == "setnonblock"
		if expectDeadlinesToWork {
			if err := unix.SetNonblock(3, true); err != nil {
				panic(err)
			}
		}
		f := os.NewFile(3, "myfile")
		err := f.SetReadDeadline(time.Now().Add(1*time.Second))
		if expectDeadlinesToWork {
			if err != nil {
				panic(err)
			}
		} else {
			if err != os.ErrNoDeadline {
				panic(err)
			}
		}
	}
`

	f, err := ioutil.TempFile("", "stdioblocking*.go")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	n, err := f.WriteString(childSrc)
	require.NoError(t, err)
	require.True(t, n == len(childSrc))
	_, err = f.Seek(0, os.SEEK_SET)
	require.NoError(t, err)

	doRun := func(t *testing.T, setNonblocking bool) {
		mode := ""
		if setNonblocking {
			mode = "setnonblock" // SHADOW
		}

		cmd := exec.Command("go", "run", f.Name(), mode)

		// set up child's stdin
		r, w, err := os.Pipe()
		require.NoError(t, err)
		defer w.Close()
		defer r.Close()
		cmd.ExtraFiles = []*os.File{r}

		// run it, non-panic is exit code 0 and hence no error
		o, err := cmd.CombinedOutput()
		t.Logf("%q", o)
		require.NoError(t, err)
	}

	t.Run("blocking_stdin", func(t *testing.T) { doRun(t, false) })
	t.Run("nonblocking_stdin", func(t *testing.T) { doRun(t, true) })

}
