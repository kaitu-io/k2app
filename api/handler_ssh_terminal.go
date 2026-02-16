package center

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for WebSocket
	},
}

// TerminalMessage represents messages between client and server
type TerminalMessage struct {
	Type string `json:"type"` // "input", "output", "resize", "error"
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// api_admin_ssh_terminal handles WebSocket SSH terminal connections
func api_admin_ssh_terminal(c *gin.Context) {
	nodeIPv4 := c.Param("ipv4")
	log.Infof(c, "SSH terminal request for node %s", nodeIPv4)

	// Find the node
	var node SlaveNode
	if err := db.Get().Where("ipv4 = ?", nodeIPv4).First(&node).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Warnf(c, "node %s not found for SSH terminal", nodeIPv4)
			c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
		} else {
			log.Errorf(c, "failed to find node %s: %v", nodeIPv4, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to find node"})
		}
		return
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf(c, "failed to upgrade WebSocket: %v", err)
		return
	}
	defer conn.Close()

	// Establish SSH connection
	sshClient, err := node.sshConnect(c)
	if err != nil {
		log.Errorf(c, "failed to connect SSH to %s: %v", nodeIPv4, err)
		sendError(conn, "Failed to connect: "+err.Error())
		return
	}
	defer sshClient.Close()

	// Create SSH session
	session, err := sshClient.NewSession()
	if err != nil {
		log.Errorf(c, "failed to create SSH session: %v", err)
		sendError(conn, "Failed to create session: "+err.Error())
		return
	}
	defer session.Close()

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		log.Errorf(c, "failed to request PTY: %v", err)
		sendError(conn, "Failed to request PTY: "+err.Error())
		return
	}

	// Get stdin pipe
	stdin, err := session.StdinPipe()
	if err != nil {
		log.Errorf(c, "failed to get stdin pipe: %v", err)
		sendError(conn, "Failed to get stdin: "+err.Error())
		return
	}

	// Get stdout pipe
	stdout, err := session.StdoutPipe()
	if err != nil {
		log.Errorf(c, "failed to get stdout pipe: %v", err)
		sendError(conn, "Failed to get stdout: "+err.Error())
		return
	}

	// Start shell
	if err := session.Shell(); err != nil {
		log.Errorf(c, "failed to start shell: %v", err)
		sendError(conn, "Failed to start shell: "+err.Error())
		return
	}

	log.Infof(c, "SSH terminal session established for node %s", nodeIPv4)

	// Use mutex for WebSocket writes (WebSocket is not thread-safe for concurrent writes)
	var wsMutex sync.Mutex

	// Goroutine: SSH stdout -> WebSocket
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				log.Debugf(c, "SSH stdout closed: %v", err)
				return
			}
			if n > 0 {
				msg := TerminalMessage{
					Type: "output",
					Data: string(buf[:n]),
				}
				wsMutex.Lock()
				if err := conn.WriteJSON(msg); err != nil {
					wsMutex.Unlock()
					log.Debugf(c, "WebSocket write error: %v", err)
					return
				}
				wsMutex.Unlock()
			}
		}
	}()

	// Main loop: WebSocket -> SSH stdin
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Debugf(c, "WebSocket read error: %v", err)
			break
		}

		var msg TerminalMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			log.Warnf(c, "invalid terminal message: %v", err)
			continue
		}

		switch msg.Type {
		case "input":
			if _, err := stdin.Write([]byte(msg.Data)); err != nil {
				log.Debugf(c, "SSH stdin write error: %v", err)
				return
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				if err := session.WindowChange(msg.Rows, msg.Cols); err != nil {
					log.Warnf(c, "failed to resize terminal: %v", err)
				}
			}
		}
	}

	// Wait for SSH output goroutine to finish
	<-done
	log.Infof(c, "SSH terminal session closed for node %s", nodeIPv4)
}

// sendError sends an error message to the WebSocket client
func sendError(conn *websocket.Conn, message string) {
	msg := TerminalMessage{
		Type: "error",
		Data: message,
	}
	conn.WriteJSON(msg)
}
