package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"runtime"
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

// these seem to be constant across many operating systems?
const maxLoginLen = 256

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

func FindMaxPathLen(os string) int {
	if os == "" {
		os = runtime.GOOS
	}
	// we have to hardcode because of reasons http://insanecoding.blogspot.com/2007/11/pathmax-simply-isnt.html
	// we could try to read in syslimits.h or another file, but who knows if they are there
	// and we don't want to parse C
	switch os {
	case "darwin":
		return 1024
	case "linux":
		return 4096
	// TODO: confirm everything below here
	case "windows":
		return 260
	case "freebsd", "openbsd", "netbsd", "plan9", "solaris", "nacl", "dragonfly", "android":
		return 1024
	default:
		panic("Unknown operating system for max path length")
	}
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func chop(b []byte, max int) []byte {
	return b[:min(len(b), max)]
}

func pad(n int) int {
	switch n % 4 {
	case 0:
		return n
	case 1:
		return n + 3
	case 2:
		return n + 2
	case 3:
		return n + 1
	}
	return 0 // unreachable
}

func max(args ...int) int {
	m := 0
	for _, arg := range args {
		if arg > m {
			m = arg
		}
	}
	return m
}

var maxTermLen = map[int]int{
	0: 20,
	1: 20,
	2: 20,
	3: 32,
	4: 32,
	5: 32,
}

func messageSize(version int, pathLen int) int {
	if pathLen == 0 {
		pathLen = FindMaxPathLen("")
	}
	header := 4 + 4 + pad(pathLen)
	createSize := 4 + 4 + 4 + 4 + 4 + pad(pathLen)*2 + pad(maxTermLen[version])
	attachSize0 := pad(maxLoginLen+1) + 4 + 4 + 4 + 4 + pad(20) + 4 + 4 + pad(maxTermLen[version]+1) + 4
	detachSize := pad(maxLoginLen+1) + 4
	commandSize := pad(maxLoginLen+1) + 4 + pad(pathLen+1) + 4 + pad(20) + pad(pathLen)
	messageSize := pad(pathLen * 2)
	switch version {
	case 0:
		return header + max(createSize, attachSize0, detachSize, commandSize, messageSize)
	case 1, 2, 3, 4:
		attachSize := attachSize0 + 4
		return header + max(createSize, attachSize, detachSize, commandSize, messageSize)
	case 5:
		createSize5 := 4 + 4 + 4 + 4 + 4 + pad(pathLen)*2 + pad(maxTermLen[version]+1)
		attachSize := attachSize0 + 4
		return header + max(createSize5, attachSize, detachSize, commandSize, messageSize)
	}
	panic(fmt.Errorf("Unknown screen protocol version %d", version))
}

func makeAttachMessage(version int, ttyName string, lines int, columns int) []byte {
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	pathLen := FindMaxPathLen("")
	uname := chop([]byte(u.Username), maxLoginLen)

	w := bytes.NewBuffer(nil)
	tty := chop([]byte(ttyName), pathLen)

	binary.Write(w, endian, uint32(('m'<<24)|('s'<<16)|('g'<<8)|version))
	binary.Write(w, endian, uint32(msgAttach))
	w.Write(tty)
	w.Write(bytes.Repeat([]byte{0}, pathLen-len(tty)))
	w.Write(bytes.Repeat([]byte{0}, pad(pathLen)-pathLen))
	w.Write(uname)
	w.Write(bytes.Repeat([]byte{0}, pad(maxLoginLen+1)-len(uname)))
	binary.Write(w, endian, int32(os.Getpid()))
	binary.Write(w, endian, uint32(0))
	binary.Write(w, endian, uint32(lines))
	binary.Write(w, endian, uint32(columns))
	w.Write(bytes.Repeat([]byte{0}, 20))
	binary.Write(w, endian, int32(0))
	binary.Write(w, endian, int32(0))
	w.WriteString("screen")
	w.Write(bytes.Repeat([]byte{0}, pad(maxTermLen[version]+1)-6))
	binary.Write(w, endian, uint32(0))

	switch version {
	case 0:
	case 1, 2, 3, 4, 5: // for attach messages these are identical except for maxTermLen identical
		binary.Write(w, endian, uint32(0)) // version one added detachfirst
	default:
		panic(fmt.Sprintf("Unknown screen protocol version %d", version))
	}
	w.Write(bytes.Repeat([]byte{0}, messageSize(version, 0)-len(w.Bytes())))
	return w.Bytes()
}

// write a message to the screen socket
func screenSocketWrite(data []byte) {
	info, err := os.Stat(os.Args[1])
	if os.IsNotExist(err) {
		fmt.Printf("File not found: %s\n", os.Args[1])
		os.Exit(1)
	} else if err != nil {
		panic(err)
	}
	var c io.WriteCloser
	if info.Mode()&os.ModeNamedPipe != 0 {
		c, err = os.OpenFile(os.Args[1], os.O_RDWR, info.Mode())
		if err != nil {
			panic(err)
		}
	} else if info.Mode()&os.ModeSocket != 0 {
		c, err = net.Dial("unix", os.Args[1])
		if err != nil {
			panic(err)
		}
	}

	defer c.Close()

	_, err = c.Write(data)
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

func attach(termFile *os.File, version int) {
	m := makeAttachMessage(version, termFile.Name(), 132, 50)
	screenSocketWrite(m)
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
	// try every version in order
	version := 5
	found := false
	for !found {
		if version < 0 {
			fmt.Printf("Could not negotiate a version\n")
			os.Exit(1)
		}
		fmt.Printf("Trying version %d\n", version)
		attach(termFile, version)
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-cont:
			// okay, we're attached
			found = true
			break
		case <-timer.C:
			// timeout, try the next version
			version--
		}
	}
	fmt.Printf("Attached with version %d\n", version)

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
