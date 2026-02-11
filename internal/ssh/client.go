package ssh

import (
	"context"
	"fmt"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Client is an alias for the ssh.Client type, so that users can import only this package.
type Client = ssh.Client

func NewClient(ctx context.Context,
	machineConfig *ssh.ClientConfig,
	machineHost string,
	machinePort int,
) (
	*ssh.Client,
	error,
) {
	client, err := DialContext(
		ctx,
		"tcp",
		fmt.Sprintf("%s:%d", machineHost, machinePort),
		machineConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("error dialing machine: %w", err)
	}
	return client, nil
}

func NewClientWithBastion(
	ctx context.Context,
	bastionConfig *ssh.ClientConfig,
	bastionHost string,
	bastionPort int,
	machineConfig *ssh.ClientConfig,
	machineHost string,
	machinePort int,
) (
	*ssh.Client,
	error,
) {
	bastionClient, err := DialContext(
		ctx,
		"tcp",
		fmt.Sprintf("%s:%d", bastionHost, bastionPort),
		bastionConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("error dialing bastion: %w", err)
	}

	bastionConn, err := bastionClient.DialContext(
		ctx,
		"tcp",
		fmt.Sprintf("%s:%d", machineHost, machinePort),
	)
	if err != nil {
		return nil, fmt.Errorf("error dialing machine via bastion: %w", err)
	}

	machineConn, chans, reqs, err := ssh.NewClientConn(
		bastionConn,
		fmt.Sprintf("%s:%d", machineHost, machinePort),
		machineConfig,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating new client connection: %w", err)
	}

	return ssh.NewClient(machineConn, chans, reqs), nil
}

func NewSSHConfig(
	ctx context.Context,
	user string,
	privateKey []byte,
) (*ssh.ClientConfig, error) {
	authMethod, err := authMethod(ctx, privateKey)
	if err != nil {
		return nil, fmt.Errorf("error creating auth method: %w", err)
	}
	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			authMethod,
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}, nil
}

func authMethod(ctx context.Context, privateKey []byte) (ssh.AuthMethod, error) {
	log := logf.FromContext(ctx)
	if len(privateKey) == 0 {
		log.V(1).Info("Using SSH agent for authentication")
		socket := os.Getenv("SSH_AUTH_SOCK")
		conn, err := net.Dial("unix", socket)
		if err != nil {
			return nil, fmt.Errorf("error dialing SSH agent: %w", err)
		}

		agentClient := agent.NewClient(conn)
		return ssh.PublicKeysCallback(agentClient.Signers), nil
	}
	log.V(1).Info("Using private key for authentication")
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("error parsing private key: %s", err)
	}
	return ssh.PublicKeys(signer), nil
}
