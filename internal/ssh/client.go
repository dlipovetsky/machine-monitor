package ssh

import (
	"context"
	"fmt"

	"golang.org/x/crypto/ssh"
)

func NewClient(ctx context.Context,
	machineIP string,
	port int,
	user string,
	privateKey []byte,
) (
	*ssh.Client,
	error,
) {
	signer, err := ssh.ParsePrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("error parsing private key: %s", err)
	}
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", machineIP, port), config)
	if err != nil {
		return nil, fmt.Errorf("error dialing SSH server: %s", err)
	}

	return client, nil
}
