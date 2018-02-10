package cmd

import (

	"github.com/spf13/cobra"

	"log"
	"os"
	"github.com/problame/go-netssh"
	"context"
)

var proxyArgs struct {
	log string
}

var proxyCmd= &cobra.Command{
	Use:   "proxy",
	Short: "proxy command to be run from authorized_keys file",
	Run: func(cmd *cobra.Command, args []string) {

		ctx := context.TODO()
		if proxyArgs.log != "" {
			logFile, err := os.OpenFile(proxyArgs.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0660)
			if err != nil {
				log.Panic(err)
			}
			log := log.New(logFile, "", log.Lshortfile|log.Ltime|log.Lmicroseconds)
			ctx = netssh.ContextWithLog(ctx, log)
		}

		err := netssh.Proxy(ctx, sock)
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)

	},
}

func init() {
	RootCmd.AddCommand(proxyCmd)
	proxyCmd.Flags().StringVar(&proxyArgs.log, "log", "", "log file (proxy must not log to stdio)")
}
