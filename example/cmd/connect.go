package cmd

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"time"

	"github.com/problame/go-netssh"
	"github.com/spf13/cobra"
)

var connectArgs struct {
	killSSHDuration           time.Duration
	waitBeforeRequestDuration time.Duration
	responseTimeout           time.Duration
	dialTimeout               time.Duration
	endpoint                  netssh.Endpoint
}

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "connect to server over SSH using proxy",
	Run: func(cmd *cobra.Command, args []string) {

		log := log.New(os.Stdout, "", log.Ltime|log.Lmicroseconds|log.Lshortfile)

		log.Printf("dialing %#v", connectArgs.endpoint)
		log.Printf("timeout %s", connectArgs.dialTimeout)
		ctx := netssh.ContextWithLog(context.TODO(), log)
		dialCtx, dialCancel := context.WithTimeout(ctx, connectArgs.dialTimeout)
		outstream, err := netssh.Dial(dialCtx, connectArgs.endpoint)
		dialCancel()
		if err == context.DeadlineExceeded {
			log.Panic("dial timeout exceeded")
		} else if err != nil {
			log.Panic(err)
		}

		defer func() {
			log.Printf("closing connection in defer")
			err := outstream.Close()
			if err != nil {
				log.Printf("error closing connection in defer: %s", err)
			}
		}()

		if connectArgs.killSSHDuration != 0 {
			go func() {
				time.Sleep(connectArgs.killSSHDuration)
				log.Printf("killing ssh process")
				outstream.CmdCancel()
			}()
		}

		time.Sleep(connectArgs.waitBeforeRequestDuration)

		log.Print("writing request")
		n, err := outstream.Write([]byte("b\n"))
		if n != 2 || err != nil {
			log.Panic(err)
		}
		log.Print("read response")
		_, err = io.CopyN(ioutil.Discard, outstream, int64(Bytecount))
		if err != nil {
			log.Panic(err)
		}

		log.Print("request for close")
		n, err = outstream.Write([]byte("a\n"))
		if n != 2 || err != nil {
			log.Panic(err)
		}
		log.Printf("wait for close message")
		var resp [2]byte
		n, err = outstream.Read(resp[:])
		if n != 2 || err != nil {
			log.Panic(err)
		}
		if bytes.Compare(resp[:], []byte("A\n")) != 0 {
			log.Panicf("unexpected close message: %v", resp)
		}
		log.Printf("received close message")

	},
}

func init() {
	RootCmd.AddCommand(connectCmd)
	connectCmd.Flags().DurationVar(&connectArgs.killSSHDuration, "killSSH", 0, "")
	connectCmd.Flags().DurationVar(&connectArgs.waitBeforeRequestDuration, "wait", 0, "")
	connectCmd.Flags().DurationVar(&connectArgs.responseTimeout, "responseTimeout", math.MaxInt64, "")
	connectCmd.Flags().DurationVar(&connectArgs.dialTimeout, "dialTimeout", math.MaxInt64, "")
	connectCmd.Flags().StringVar(&connectArgs.endpoint.Host, "ssh.host", "", "")
	connectCmd.Flags().StringVar(&connectArgs.endpoint.User, "ssh.user", "", "")
	connectCmd.Flags().StringVar(&connectArgs.endpoint.IdentityFile, "ssh.identity", "", "")
	connectCmd.Flags().Uint16Var(&connectArgs.endpoint.Port, "ssh.port", 22, "")
}
