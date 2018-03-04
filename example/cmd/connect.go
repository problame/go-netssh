package cmd

import (

	"github.com/spf13/cobra"
	"log"
	"io"
	"io/ioutil"
	"time"
	"context"
	"os"
	"github.com/problame/go-netssh"
	"math"
	"github.com/problame/go-rwccmd"
)

var connectArgs struct {
	killSSHDuration time.Duration
	waitBeforeRequestDuration time.Duration
	responseTimeout time.Duration
	dialTimeout time.Duration
	endpoint netssh.Endpoint

}

var connectCmd = &cobra.Command{
	Use:   "connect",
	Short: "connect to server over SSH using proxy",
	Run: func(cmd *cobra.Command, args []string) {

		log := log.New(os.Stdout, "", log.Ltime|log.Lmicroseconds|log.Lshortfile)

		log.Print("dialing %#v", connectArgs.endpoint)
		log.Printf("timeout %s", connectArgs.dialTimeout)
		ctx := netssh.ContextWithLog(context.TODO(), log)
		ctx = rwccmd.ContextWithLog(ctx, log)
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
			go func(){
				time.Sleep(connectArgs.killSSHDuration)
				log.Printf("killing ssh process")
				if err := outstream.Cmd().Kill(); err != nil {
					log.Printf("error killing ssh process: %s", err)
				}
			}()
		}

		time.Sleep(connectArgs.waitBeforeRequestDuration)

		var dl time.Time
		if connectArgs.responseTimeout > 0 {
			dl = time.Now().Add(connectArgs.responseTimeout)
		} else {
			dl = time.Time{}
		}
		outstream.Cmd().CloseAtDeadline(dl)

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
		log.Print("writing close request")
		n, err = outstream.Write([]byte("a\n"))
		if n != 2 || err != nil {
			log.Panic(err)
		}
	},
}

func init() {
	RootCmd.AddCommand(connectCmd)
	connectCmd.Flags().DurationVar(&connectArgs.killSSHDuration, "killSSH",0, "")
	connectCmd.Flags().DurationVar(&connectArgs.waitBeforeRequestDuration, "wait",0, "")
	connectCmd.Flags().DurationVar(&connectArgs.responseTimeout, "responseTimeout",math.MaxInt64, "")
	connectCmd.Flags().DurationVar(&connectArgs.dialTimeout, "dialTimeout",math.MaxInt64, "")
	connectCmd.Flags().StringVar(&connectArgs.endpoint.Host, "ssh.host", "", "")
	connectCmd.Flags().StringVar(&connectArgs.endpoint.User, "ssh.user", "", "")
	connectCmd.Flags().StringVar(&connectArgs.endpoint.IdentityFile, "ssh.identity", "", "")
	connectCmd.Flags().Uint16Var(&connectArgs.endpoint.Port, "ssh.port", 22, "")
}
