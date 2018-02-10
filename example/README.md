# How to run the example

```
cd $GOPATH/github.com/problame/go-netssh/example
go build
# confirm the following questions with empty passphrase
ssh-keygen -t ed25519 -f exampleident
PUBKEY=$(cat exampleident.pub)
ENTRY="command=\"$(pwd)/example proxy --log /tmp/netssh_proxy.log\",restrict $PUBKEY"
echo
echo "Add the following line to your authorized_keys file: "
echo
echo $ENTRY
```

Then, in one terminal, run

```
./example serve
```

And in another

```
touch /tmp/netssh_proxy.log
tail -f
```

And in another

```
./example connect --ssh.user $(id -nu) --ssh.identity exampleident --ssh.host localhost
```

You should see log activity in all 3 terminals.
