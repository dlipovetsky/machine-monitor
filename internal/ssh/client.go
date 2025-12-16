package ssh

import (
	"context"
	"fmt"

	"golang.org/x/crypto/ssh"
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
	user string,
	privateKey []byte,
) (*ssh.ClientConfig, error) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("error parsing private key: %s", err)
	}
	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}, nil
}
