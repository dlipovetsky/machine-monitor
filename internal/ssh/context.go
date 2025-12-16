// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ssh

import (
	"context"
	"net"

	"golang.org/x/crypto/ssh"
)

// NOTE This code is from https://github.com/golang/crypto/commit/29643602415fafeae2dbf472db6a3956732571bd,
// which is part of a PR to add a DialContext function to the ssh package: https://github.com/golang/crypto/pull/280

// DialContext starts a client connection to the given SSH server. It is a
// convenience function that connects to the given network address,
// initiates the SSH handshake, and then sets up a Client.
//
// The provided Context must be non-nil. If the context expires before the
// connection is complete, an error is returned. Once successfully connected,
// any expiration of the context will not affect the connection.
//
// See [Dial] for additional information.
func DialContext(
	ctx context.Context,
	network, addr string,
	config *ssh.ClientConfig,
) (*Client, error) {
	d := net.Dialer{
		Timeout: config.Timeout,
	}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	type result struct {
		client *Client
		err    error
	}
	ch := make(chan result)
	go func() {
		var client *Client
		c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
		if err == nil {
			client = ssh.NewClient(c, chans, reqs)
		}
		select {
		case ch <- result{client, err}:
		case <-ctx.Done():
			if client != nil {
				client.Close() //nolint:errcheck // We are already returning the context cause.
			}
		}
	}()
	select {
	case res := <-ch:
		return res.client, res.err
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}
