package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kr/pty"
)

var devURL = "ws://localhost:8080/tty?id="
var prodURL = "wss://screen.run/tty?id="
var viewDevURL = "http://localhost:8080/view?id="
var viewProdURL = "https://screen.run/view?id="

var protocol = "uploadtty"

var endian = binary.LittleEndian

const term = "screen"

// these are platform-dependent :/

// MacOS 10.12, 64-bit
const maxPathLen = 1024
const maxLoginLen = 256
const maxTermLen = 32
const dataSize = 2340
const messageSize = 3372

// ubuntu 16.04 x86-64
// MAXPATHLEN 4096
// MAXLOGINLEN 256
// MAXTERMLEN 32
// message 12588
// create 8248
// attach 348
// detach 264
// command 8484
// message 8192
// msgVersion = 0

const msgVersion = 5
const msgRevision = ('m' << 24) | ('s' << 16) | ('g' << 8) | msgVersion
const msgCreate = 0
const msgError = 1
const msgAttach = 2
const msgCont = 3
const msgDetach = 4
const msgPowDetach = 5
const msgWinch = 6
const msgHangup = 7
const msgCommand = 8
const msgQuery = 9

const sigLock = syscall.SIGUSR2
const sigBye = syscall.SIGHUP
const sigPowerBye = syscall.SIGUSR1

type screenMessage struct {
	ProtocolRevision uint32
	Type             uint32
	Mtty             [maxPathLen]byte
	Data             [dataSize]byte
}

type screenMessageAttach struct {
	Auser       [maxLoginLen + 1]byte
	padding     [(4 - ((maxLoginLen + 1) % 4)) % 4]byte
	Apid        int32
	Adaptflag   uint32
	Lines       uint32
	Columns     uint32
	Preselect   [20]byte
	Esc         int32
	MetaEsc     int32
	Envterm     [maxTermLen + 1]byte
	padding2    [(4 - ((maxTermLen + 1) % 4)) % 4]byte
	Encoding    uint32
	Detachfirst uint32
}

func serialize(m *screenMessage, data interface{}) []byte {
	dataOut := bytes.NewBuffer(nil)
	err := binary.Write(dataOut, endian, data)
	if err != nil {
		panic(err)
	}

	copy(m.Data[:], dataOut.Bytes())

	messageOut := bytes.NewBuffer(nil)
	err = binary.Write(messageOut, endian, *m)
	if err != nil {
		panic(err)
	}
	dest := messageOut.Bytes()
	return dest
}

type screenMessageMessage [2048]byte

// write a message to the screen socket
func write(m *screenMessage, data interface{}) {
	c, err := net.Dial("unix", os.Args[1])
	if err != nil {
		panic(err)
	}
	defer c.Close()

	out := serialize(m, data)
	_, err = c.Write(out)
	if err != nil {
		panic(err)
	}
}

func forwardTty(pt *os.File, conn *websocket.Conn) {
	last := time.Now().UnixNano()
	buffer := make([]byte, 1024)
	for {
		n, err := pt.Read(buffer[12:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		if n == 0 {
			time.Sleep(1 * time.Millisecond)
		}
		now := time.Now().UnixNano()
		diff := now - last
		diffSeconds := int32(diff / 1e9)
		diffMicros := int32((diff / 100) % 1e6)
		// unlilkey, but time is weird
		if diffMicros < 0 {
			diffMicros += 1e6
		}
		binary.Write(bytes.NewBuffer(buffer[:0]), binary.LittleEndian, diffSeconds)
		binary.Write(bytes.NewBuffer(buffer[:4]), binary.LittleEndian, diffMicros)
		binary.Write(bytes.NewBuffer(buffer[:8]), binary.LittleEndian, int32(n))
		w, err := conn.NextWriter(websocket.BinaryMessage)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		w.Write(buffer[:n+12])
		w.Close()
		last = now
	}
}

func signalHandler(ch chan os.Signal, termFile *os.File, cont chan bool, closed chan bool) {
	for {
		sig := <-ch
		if sig == syscall.SIGINT {
			fmt.Println("Caught SIGINT, shutting down")
			// close everything gracefully
			termFile.Close()
			closed <- true
			continue
		}

		if sig == syscall.SIGCONT {
			signal.Reset(syscall.SIGCONT)
			cont <- true
			continue
		}

		if sig == syscall.SIGHUP {
			closed <- true
			os.Exit(0)
		}
		fmt.Printf("Unknown signal %v\n", sig)
	}
}

// process close messages and discard others
func processWebsocketIncoming(conn *websocket.Conn, closed chan bool) {
	for {
		if _, _, err := conn.NextReader(); err != nil {
			fmt.Printf("WebSocket returned %v\n", err)
			conn.Close()
			break
		}
	}
	closed <- true
}

func attach(termFile *os.File) {
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	m := new(screenMessage)
	m.ProtocolRevision = msgRevision
	m.Type = msgAttach
	copy(m.Mtty[:], []byte(termFile.Name()))
	m.Mtty[len(termFile.Name())] = 0
	attach := screenMessageAttach{
		Apid:        int32(os.Getpid()),
		Esc:         -1,
		MetaEsc:     -1,
		Detachfirst: msgAttach,
		Adaptflag:   0,
		Lines:       50,
		Columns:     132,
	}
	copy(attach.Auser[:], []byte(u.Username))
	attach.Auser[len(u.Name)] = 0
	copy(attach.Envterm[:], []byte(term))
	attach.Envterm[len(term)] = 0
	write(m, attach)
}

func url() string {
	if os.Getenv("ENV") == "dev" {
		return devURL
	}
	return prodURL
}

func viewURL() string {
	if os.Getenv("ENV") == "dev" {
		return viewDevURL
	}
	return viewProdURL
}

func newID() string {
	buffer := make([]byte, 15)
	rand.Read(buffer)
	id := base32.StdEncoding.EncodeToString(buffer)
	return id
}

func newWebSocket(id string) *websocket.Conn {
	headers := http.Header{}
	hostname, _ := os.Hostname()
	headers.Add("Origin", hostname)
	headers.Add("Sec-WebSocket-Protocol", protocol)
	conn, _, err := websocket.DefaultDialer.Dial(url()+id, headers)
	if err != nil {
		panic(err)
	}
	return conn
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("screenrun plays GNU screen sessions on a remote web server")
		fmt.Println("usage: ./screenrun screenfile")
		fmt.Println("\nExample: ./screenrun $HOME/.screen/*   # picks up the first screen")
		os.Exit(1)
	}

	pt, termFile, err := pty.Open()
	if err != nil {
		panic(err)
	}

	defer termFile.Close()
	defer pt.Close()

	ch := make(chan os.Signal)

	signal.Notify(ch, sigBye, sigPowerBye, sigLock, syscall.SIGINT, syscall.SIGWINCH, syscall.SIGSTOP, syscall.SIGALRM, syscall.SIGCONT)

	cont := make(chan bool)
	closed := make(chan bool)
	go signalHandler(ch, termFile, cont, closed)

	fmt.Printf("Attaching to screen %s...\n", os.Args[1])
	attach(termFile)
	<-cont
	fmt.Printf("Attached\n")

	id := newID()

	conn := newWebSocket(id)

	// wait for a message from the server to know that it is setup
	_, _, err = conn.ReadMessage()
	if err != nil {
		panic(err)
	}

	go forwardTty(pt, conn)

	fmt.Printf("View at %s\n", viewURL()+id)

	go processWebsocketIncoming(conn, closed)

	// wait for the connection to close
	<-closed
	os.Exit(0)
}
