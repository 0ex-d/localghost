// ghost.restore , the OFFLINE half of the backup system. Two subcommands:
//
//	ghost.restore keygen
//	    Generates an X25519 keypair. Prints the PRIVATE key hex to stdout (write it down, store
//	    it away from the box , it is the only thing that can ever read a backup) and writes the
//	    public half to ./backup.pub for copying to /var/lib/ghost/backup.pub on the box.
//
//	ghost.restore unseal <file.tar.seal> < /dev/stdin-private-key-hex > out.tar
//	    Reads the private key hex from stdin (never argv , argv is world-readable in /proc),
//	    decrypts the sealed backup, writes the tar to stdout. Pipe to `tar -x` to restore.
//
// The box never runs this. The box holds only backup.pub and can seal but never unseal , that is
// the entire point.
package main

import (
	"bufio"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const magic = "GHSTBK1"

func main() {
	if len(os.Args) < 2 {
		fatal("usage: ghost.restore keygen | ghost.restore unseal <file.tar.seal>")
	}
	switch os.Args[1] {
	case "keygen":
		keygen()
	case "unseal":
		if len(os.Args) < 3 {
			fatal("unseal requires the sealed file path")
		}
		unseal(os.Args[2])
	default:
		fatal("unknown subcommand " + os.Args[1])
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "ghost.restore: "+msg)
	os.Exit(1)
}

func keygen() {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		fatal("keygen: " + err.Error())
	}
	pubHex := hex.EncodeToString(priv.PublicKey().Bytes())
	if err := os.WriteFile("backup.pub", []byte(pubHex+"\n"), 0o600); err != nil {
		fatal("write backup.pub: " + err.Error())
	}
	fmt.Fprintln(os.Stderr, "public key written to ./backup.pub , copy it to /var/lib/ghost/backup.pub on the box")
	fmt.Fprintln(os.Stderr, "PRIVATE key follows on stdout , store it OFFLINE, it is the only thing that can read a backup:")
	fmt.Println(hex.EncodeToString(priv.Bytes()))
}

func unseal(path string) {
	keyHex, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && err != io.EOF {
		fatal("read private key from stdin: " + err.Error())
	}
	privBytes, err := hex.DecodeString(strings.TrimSpace(keyHex))
	if err != nil || len(privBytes) != 32 {
		fatal("private key must be 64 hex chars on stdin")
	}
	priv, err := ecdh.X25519().NewPrivateKey(privBytes)
	if err != nil {
		fatal("private key: " + err.Error())
	}
	f, err := os.Open(path)
	if err != nil {
		fatal(err.Error())
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	head := make([]byte, len(magic)+32)
	if _, err := io.ReadFull(r, head); err != nil {
		fatal("header: " + err.Error())
	}
	if string(head[:len(magic)]) != magic {
		fatal("not a ghost backup (bad magic)")
	}
	ephPub, err := ecdh.X25519().NewPublicKey(head[len(magic):])
	if err != nil {
		fatal("ephemeral key: " + err.Error())
	}
	shared, err := priv.ECDH(ephPub)
	if err != nil {
		fatal("ecdh: " + err.Error())
	}
	salt := append(append([]byte{}, ephPub.Bytes()...), priv.PublicKey().Bytes()...)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf.New(sha256.New, shared, salt, []byte("ghost-backup-v1")), key); err != nil {
		fatal("hkdf: " + err.Error())
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		fatal(err.Error())
	}
	out := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer out.Flush()
	var chunk uint64
	var lenBuf [4]byte
	for {
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			if err == io.EOF {
				fatal("truncated backup: end-of-stream chunk never arrived")
			}
			fatal("chunk length: " + err.Error())
		}
		n := binary.BigEndian.Uint32(lenBuf[:])
		if n > (1<<20)+64 {
			fatal("chunk too large , corrupt or not a backup")
		}
		ct := make([]byte, n)
		if _, err := io.ReadFull(r, ct); err != nil {
			fatal("chunk read: " + err.Error())
		}
		// Try as a middle chunk first, then as the final chunk (0x01 marker).
		nonce := make([]byte, aead.NonceSize())
		binary.BigEndian.PutUint64(nonce[:8], chunk)
		pt, derr := aead.Open(nil, nonce, ct, nil)
		final := false
		if derr != nil {
			nonce[11] = 1
			pt, derr = aead.Open(nil, nonce, ct, nil)
			final = true
		}
		if derr != nil {
			fatal(fmt.Sprintf("chunk %d failed authentication , wrong key or corrupt file", chunk))
		}
		if _, err := out.Write(pt); err != nil {
			fatal("write: " + err.Error())
		}
		chunk++
		if final {
			return
		}
	}
}
