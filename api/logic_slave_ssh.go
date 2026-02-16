package center

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/wordgate/qtoolkit/log"
	"golang.org/x/crypto/ssh"
)

const (
	SSHPort    = 1022
	SSHUser    = "ubuntu"
	SSHTimeout = 30 * time.Second
)

// SSHResult represents the result of an SSH command execution
type SSHResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SSHExec executes a command on the slave node via SSH
func (node *SlaveNode) SSHExec(ctx context.Context, command string) (*SSHResult, error) {
	log.Infof(ctx, "SSH exec on %s: %s", node.Ipv4, command)

	client, err := node.sshConnect(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	err = session.Run(command)

	result := &SSHResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}

	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			result.ExitCode = exitErr.ExitStatus()
		} else {
			return result, fmt.Errorf("SSH command failed: %w", err)
		}
	}

	return result, nil
}

// SSHCopyFile copies content to a file on the remote node
func (node *SlaveNode) SSHCopyFile(ctx context.Context, content []byte, remotePath string) error {
	log.Infof(ctx, "SSH copy to %s:%s", node.Ipv4, remotePath)

	client, err := node.sshConnect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	session.Stdin = bytes.NewReader(content)
	if err := session.Run(fmt.Sprintf("cat > %s", remotePath)); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// sshConnect establishes an SSH connection to the node
func (node *SlaveNode) sshConnect(ctx context.Context) (*ssh.Client, error) {
	keypair, err := getSSHKeypair(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH keypair: %w", err)
	}

	signer, err := ssh.ParsePrivateKey([]byte(keypair.PrivateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         SSHTimeout,
	}

	addr := fmt.Sprintf("%s:%d", node.Ipv4, SSHPort)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	return client, nil
}
