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

var url = "ws://localhost:8080/tty?id="
var viewUrl = "http://localhost:8080/view?id="
var protocol = "uploadtty"

var endian = binary.LittleEndian

const term = "screen"

const DefaultEsc = 'a' & 037
const DefaultMetaEsc = 'a' & 037
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

// these are platform-dependent :/

// MacOS 10.12, 64-bit
const maxPathLen = 1024
const maxLoginLen = 256
const maxTermLen = 32
const dataSize = 2340
const messageSize = 3372

type screenMessage struct {
	ProtocolRevision uint32
	Type             uint32
	Mtty             [maxPathLen]byte
	Data             [dataSize]byte
}

type screenMessageCreate struct {
	Lflag      uint32
	Aflag      bool
	Flowflag   uint32
	Hheight    uint32
	Nargs      uint32
	Line       [maxPathLen]byte
	Dir        [maxPathLen]byte
	Screenterm [maxTermLen + 1]byte
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

type screenMessageDetach struct {
	Duser [maxLoginLen + 1]byte
	Dpid  int32
}

type screenMessageCommand struct {
	Auser     [maxLoginLen + 1]byte
	padding   [(4 - ((maxLoginLen + 1) % 4)) % 4]byte
	Nargs     uint32
	Cmd       [maxPathLen + 1]byte
	padding2  [(4 - ((maxPathLen + 1) % 4)) % 4]byte
	Apid      int32
	Preselect [20]byte
	Writeback [maxPathLen]byte
}

type screenMessageMessage [2048]byte

// here is the original GNU screen message struct:
//
// typedef struct Message Message;
// struct Message {
// 	int protocol_revision;	/* reduce harm done by incompatible messages */
// 	int type;
// 	char m_tty[MAXPATHLEN];	/* ttyname */
// 	union {
// 		struct {
// 			int lflag;
// 			bool aflag;
// 			int flowflag;
// 			int hheight;			/* size of scrollback buffer */
// 			int nargs;
// 			char line[MAXPATHLEN];
// 			char dir[MAXPATHLEN];
// 			char screenterm[MAXTERMLEN + 1];/* is screen really "screen" ? */
// 		} create;
// 		struct {
// 			char auser[MAXLOGINLEN + 1];	/* username */
// 			pid_t apid;			/* pid of frontend */
// 			int adaptflag;			/* adapt window size? */
// 			int lines, columns;		/* display size */
// 			char preselect[20];
// 			int esc;			/* his new escape character unless -1 */
// 			int meta_esc;			/* his new meta esc character unless -1 */
// 			char envterm[MAXTERMLEN + 1];	/* terminal type */
// 			int encoding;			/* encoding of display */
// 			int detachfirst;		/* whether to detach remote sessions first */
// 		} attach;
// 		struct {
// 			char duser[MAXLOGINLEN + 1];	/* username */
// 			pid_t dpid;			/* pid of frontend */
// 		} detach;
// 		struct {
// 			char auser[MAXLOGINLEN + 1];	/* username */
// 			int nargs;
// 			char cmd[MAXPATHLEN + 1];	/* command */
// 			pid_t apid;		/* pid of frontend */
// 			char preselect[20];
// 			char writeback[MAXPATHLEN];	/* The socket to write the result.
// 							   Only used for MSG_QUERY */
// 			} command;
// 		char message[MAXPATHLEN * 2];
// 	} m;
// };

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

func main() {
	if len(os.Args) != 2 {
		fmt.Println("screenrun plays GNU screen sessions on a remote web server")
		fmt.Println("usage: ./screenrun screenfile")
		fmt.Println("\nExample: ./screenrun $HOME/.screen/*   # picks up the first screen")
		os.Exit(1)
	}

	pt, file, err := pty.Open()
	if err != nil {
		panic(err)
	}

	defer file.Close()
	defer pt.Close()

	u, err := user.Current()
	if err != nil {
		panic(err)
	}

	m := new(screenMessage)
	m.ProtocolRevision = msgRevision
	m.Type = msgAttach
	copy(m.Mtty[:], []byte(file.Name()))
	m.Mtty[len(file.Name())] = 0
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
	ch := make(chan os.Signal)

	const SIG_LOCK = syscall.SIGUSR2
	const SIG_BYE = syscall.SIGHUP
	const SIG_POWER_BYE = syscall.SIGUSR1
	signal.Notify(ch, SIG_BYE, SIG_POWER_BYE, SIG_LOCK, syscall.SIGINT, syscall.SIGWINCH, syscall.SIGSTOP, syscall.SIGALRM, syscall.SIGCONT)

	var conn *websocket.Conn

	cont := make(chan bool)
	go func() {
		for {
			sig := <-ch
			if sig == syscall.SIGINT {
				fmt.Println("Caught SIGINT, shutting down")
				// close everything gracefully
				file.Close()
				if conn != nil {
					w, err := conn.NextWriter(websocket.CloseMessage)
					if err != nil {
						fmt.Printf("Error: %v\n", err)
						os.Exit(1)
					}
					w.Write(websocket.FormatCloseMessage(websocket.CloseNormalClosure, "SIGINT"))
					w.Close()
				}
				os.Exit(0)
			}

			if sig == syscall.SIGCONT {
				signal.Reset(syscall.SIGCONT)
				cont <- true
				continue
			}

			if sig == syscall.SIGHUP {
				// close connection gracefully
				if conn != nil {
					conn.Close()
				}
				os.Exit(0)
			}
			fmt.Printf("Unknown signal %v\n", sig)
		}
	}()

	fmt.Printf("Connecting to screen %s...\n", os.Args[1])
	write(m, attach)
	<-cont
	fmt.Printf("Connected\n")

	buffer := make([]byte, 15)
	rand.Read(buffer)
	id := base32.StdEncoding.EncodeToString(buffer)

	headers := http.Header{}
	hostname, _ := os.Hostname()
	headers.Add("Origin", hostname)
	headers.Add("Sec-WebSocket-Protocol", protocol)
	conn, _, err = websocket.DefaultDialer.Dial(url+id, headers)
	if err != nil {
		panic(err)
	}

	// wait for a message from the server to know that it is setup
	_, _, err = conn.ReadMessage()
	if err != nil {
		panic(err)
	}

	last := time.Now().UnixNano()

	go func() {
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
	}()

	fmt.Printf("View at %s\n", viewUrl+id)

	// process close messages and discard others
	for {
		if _, _, err := conn.NextReader(); err != nil {
			fmt.Printf("WebSocket returned %v\n", err)
			conn.Close()
			break
		}
	}
	// our connection closed
	os.Exit(0)
}
