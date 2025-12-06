package journald

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/ssh"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// StreamFromRemote streams the journal from the remote machine to the local machine.
// If the local journal file does not exist, it will remove the remote journald cursor file
// before streaming the journal, to ensure that entire journal is streamed.
// The function will return if the remote command fails, if the SSH session fails,
// or if the context is cancelled.
func StreamFromRemote(
	ctx context.Context,
	client *ssh.Client,
	cursorFilePath, localJournalFilePath string,
) error {
	log := logf.FromContext(ctx)

	// Check if the local journal file exists. If the local journal file does not exist, we should
	// ensure the remote journald cursor file does not exist. If the remote journald cursor file exists,
	// then the local journal file will only receive entries from after the cursor.
	_, err := os.Stat(localJournalFilePath)
	if os.IsNotExist(err) {
		log.V(1).Info(
			"local journal file does not exist, removing remote journald cursor file",
			"cursorFilePath",
			cursorFilePath,
		)
		resetCursorErr := resetCursorFile(ctx, client, cursorFilePath)
		if resetCursorErr != nil {
			return fmt.Errorf("failed to reset journald cursor file: %w", resetCursorErr)
		}
	}

	streamErr := stream(ctx, client, cursorFilePath, localJournalFilePath)
	if streamErr != nil {
		return fmt.Errorf("failed to stream journal from remote: %w", streamErr)
	}
	return nil
}

func streamJournalAsRootCommand(cursorFilePath string) string {
	return fmt.Sprintf("sudo journalctl --follow --no-tail --cursor-file=%s", cursorFilePath)
}

func removeCursorFileCommand(cursorFilePath string) string {
	// We use --force so that the command succeeds even if the file does not exist.
	return fmt.Sprintf("rm --force %s", cursorFilePath)
}

func resetCursorFile(ctx context.Context, client *ssh.Client, cursorFilePath string) error {
	log := logf.FromContext(ctx)

	session, createSessionErr := client.NewSession()
	if createSessionErr != nil {
		return fmt.Errorf("failed to create new SSH session: %w", createSessionErr)
	}

	sshErrWriter := bytes.Buffer{}
	session.Stdout = io.Discard
	session.Stderr = &sshErrWriter

	command := removeCursorFileCommand(cursorFilePath)
	log.V(1).Info("running command on remote host", "command", command)
	resetCursorErr := session.Run(command)
	if resetCursorErr != nil {
		return fmt.Errorf(
			"failed to run command %q on remote host: %w: stderr=%q",
			command,
			resetCursorErr,
			sshErrWriter.String(),
		)
	}
	closeSessionErr := session.Close()
	if closeSessionErr != nil && closeSessionErr != io.EOF {
		// EOF is expected when the session is closed. See https://github.com/golang/go/issues/38115 for more details.
		log.Error(closeSessionErr, "failed to close session")
	}
	return nil
}

func stream(
	ctx context.Context,
	client *ssh.Client,
	cursorFilePath, localJournalFilePath string,
) error {
	log := logf.FromContext(ctx)

	session, createSessionErr := client.NewSession()
	if createSessionErr != nil {
		return fmt.Errorf("failed to create new SSH session: %w", createSessionErr)
	}

	// We only append to the local journal file.
	outWriter, openFileErr := os.OpenFile(
		localJournalFilePath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if openFileErr != nil {
		return fmt.Errorf("failed to open local journal file: %w", openFileErr)
	}

	sshErrWriter := bytes.Buffer{}
	session.Stdout = outWriter
	session.Stderr = &sshErrWriter

	command := streamJournalAsRootCommand(cursorFilePath)
	log.V(1).Info("running command on remote host", "command", command)
	sessionErr := session.Start(command)
	if sessionErr != nil {
		closeOutWriterErr := outWriter.Close()
		if closeOutWriterErr != nil {
			log.Error(closeOutWriterErr, "failed to close local journal file")
		}
		return fmt.Errorf(
			"failed to run command %q on remote host: %w: stderr=%q",
			command,
			sessionErr,
			sshErrWriter.String(),
		)
	}

	// Wait for the session to finish.
	// If the context is cancelled, send a signal to the session to interrupt it.
	// If we interrupt the session, we expect the Wait to return an error, so we ignore it.

	errCh := make(chan error)
	go func() {
		errCh <- session.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-errCh:
		// The session finished.
	case <-ctx.Done():
		// Context cancelled, so we need to send a signal to the session to interrupt it.
		signalErr := session.Signal(ssh.SIGTERM)
		if signalErr != nil {
			log.Error(signalErr, "failed to send signal to SSH session")
			// If we fail to send the signal, we have to close the session without waiting for it.
			// Otherwise, we may wait forever.
			closeSessionErr := session.Close()
			if closeSessionErr != nil {
				log.Error(closeSessionErr, "failed to close SSHsession")
			}
		}
		// Wait for the signal to terminate the session, and the goroutine to finish.
		waitErr = <-errCh
	}

	closeSessionErr := session.Close()
	if closeSessionErr != nil && closeSessionErr != io.EOF {
		// EOF is expected when the session is closed. See https://github.com/golang/go/issues/38115 for more details.
		log.Error(closeSessionErr, "failed to close SSH session")
	}
	closeOutWriterErr := outWriter.Close()
	if closeOutWriterErr != nil {
		log.Error(closeOutWriterErr, "failed to close local journal file")
	}

	if ctx.Err() == nil && waitErr != nil {
		// If the context was not cancelled, then we have an unexpected error.
		return fmt.Errorf("unexpected error running journalctl on remote host: %w", waitErr)
	}
	return nil
}
