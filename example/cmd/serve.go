package cmd

import (

	"github.com/spf13/cobra"
	"io"
	"os"
	"log"
	"time"
	"github.com/problame/go-netssh"
)

// serveCmd represents the remotesrv command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "run server in foreground",
	Run: func(cmd *cobra.Command, args []string) {

		if err := os.Remove(sock); err != nil && !os.IsNotExist(err) {
			log.Fatalf("cannot remove (stale?) socket we want to bind to: %s", err)
		}

		log.Print("listening")
		listener, err := netssh.Listen(sock)
		if err != nil {
			log.Panic(err)
		}

		handleConn := func() {

			log.Print("accepting")

			rwc, err := listener.Accept()
			defer rwc.Close()


			log.Print("urandom")
			rand, err := os.Open("/dev/urandom")
			if err != nil {
				log.Panic(err)
			}
			defer rand.Close()

			log.Print("starting")

			var msg [2]byte
		out:
			for {

				log.Print("begin")
				_, err := io.ReadFull(rwc, msg[:])
				if err != nil {
					log.Print("error reading: " + err.Error())
					break out
				}

				switch msg[0] {
				case byte('a'):
					log.Print("closing conn after quit")
					_, err := io.WriteString(rwc, "we quit now")
					if err != nil {
						log.Print("error writing " + err.Error())
					}
					break out
				default:
					log.Printf("msg: %s", string(msg[:]))
					for i := 0; i < Bytecount; {
						n, err := io.CopyN(rwc, rand, int64(1024))
						if err != nil {
							log.Print("error writing: " + err.Error())
							break out
						}
						i += int(n)
						time.Sleep(time.Second)
					}
				}

			}

		}

		for {
			handleConn()
		}

		log.Print("exiting")
	},
}

func init() {
	RootCmd.AddCommand(serveCmd)
}
