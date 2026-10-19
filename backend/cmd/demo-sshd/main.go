// Command demo-sshd runs a throwaway SSH/SFTP server for local development.
//
// It exists so the app can be exercised end-to-end (connect, browse, upload,
// download) without a real server. It is not part of the shipped app and must
// never be pointed at a machine you care about: it accepts one credential and
// exposes the whole local file system over SFTP.
//
//	go run ./cmd/demo-sshd --addr 127.0.0.1:2222 --user demo --password demo
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	var (
		addr     = flag.String("addr", "127.0.0.1:2222", "listen address")
		user     = flag.String("user", "demo", "accepted username")
		password = flag.String("password", "demo", "accepted password")
		root     = flag.String("root", "", "initial working directory for SFTP sessions")
	)
	flag.Parse()

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if meta.User() == *user && string(pass) == *password {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials")
		},
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		log.Fatal(err)
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("demo ssh/sftp listening on %s (user=%s password=%s)", ln.Addr(), *user, *password)
	log.Printf("host key fingerprint: %s", ssh.FingerprintSHA256(signer.PublicKey()))

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go handle(conn, cfg, *root)
	}
}

func handle(conn net.Conn, cfg *ssh.ServerConfig, root string) {
	defer conn.Close()
	serverConn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	defer serverConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go serveSession(channel, requests, root)
	}
}

func serveSession(channel ssh.Channel, requests <-chan *ssh.Request, root string) {
	defer channel.Close()
	for req := range requests {
		switch req.Type {
		case "subsystem":
			if subsystem(req.Payload) != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			options := []sftp.ServerOption{}
			if root != "" {
				options = append(options, sftp.WithServerWorkingDirectory(root))
			}
			server, err := sftp.NewServer(channel, options...)
			if err != nil {
				log.Printf("sftp server: %v", err)
				return
			}
			if err := server.Serve(); err != nil {
				log.Printf("sftp serve: %v", err)
			}
			_ = server.Close()
			return
		case "env", "pty-req", "window-change":
			_ = req.Reply(true, nil)
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func subsystem(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(payload[:4])
	if int(n) > len(payload)-4 {
		return ""
	}
	return string(payload[4 : 4+n])
}

var _ = os.Stdout
