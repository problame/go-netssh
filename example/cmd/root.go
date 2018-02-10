package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "netssh",
	Short: "A demo of netssh",
}

func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

var Bytecount int

const sock = "/tmp/netssh_example.sock"

func init() { 
	RootCmd.PersistentFlags().IntVar(&Bytecount, "bytes", 1, "number of bytes to send")
}
