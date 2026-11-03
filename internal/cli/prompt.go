package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/narcisbodea/smstunnel-cli/internal/js"
)

// readSecret reads the API key: hidden (shown as *) when typed or pasted in a terminal, or the first line of
// stdin when it is piped (echo "$KEY" | smstunnel login).
func readSecret(c *runContext, question string) (string, error) {
	stderr := writer{c.env.Stderr}
	if c.env.Stdin.IsTerminal() {
		return readHidden(c, question)
	}
	stderr.write(question)
	line, err := readFirstLine(c.env.Stdin)
	stderr.write("\n")
	return line, err
}

func readHidden(c *runContext, question string) (string, error) {
	out := writer{c.env.Stderr}
	out.write(question)
	keys, restore, err := c.env.Stdin.Raw()
	if err != nil {
		return "", err
	}
	var value []uint16 // UTF-16, as the Node version keeps it (Backspace removes one code unit)
	finish := func(stopped bool) (string, error) {
		restore()
		out.write("\n")
		if stopped {
			return "", &CliError{Message: "Stopped.", ExitCode: exitInterrupted}
		}
		return js.Trim(js.FromUTF16(value)), nil
	}
	var pending []byte
	buf := make([]byte, 256)
	for {
		n, err := keys.Read(buf)
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			if !utf8.FullRune(pending) && err == nil {
				break // the rest of this character comes with the next read
			}
			r, size := utf8.DecodeRune(pending)
			pending = pending[size:]
			switch {
			case r == '\r' || r == '\n' || r == 0x04:
				return finish(false)
			case r == 0x03:
				return finish(true)
			case r == 0x7F || r == '\b':
				if len(value) > 0 {
					value = value[:len(value)-1]
					out.write("\b \b")
				}
			case r < ' ':
				// other control keys and escape sequences
			default:
				value = append(value, js.UTF16(string(r))...)
				out.write("*")
			}
		}
		if err != nil {
			// The terminal closed: Node.js waits for a key that never comes; here the input simply ends.
			return finish(false)
		}
	}
}

// readFirstLine is the first line of stdin that is not blank, trimmed (or all of it, up to the end, when it has no
// newline).
func readFirstLine(in io.Reader) (string, error) {
	r := bufio.NewReader(in)
	var seen []string
	for {
		line, err := r.ReadString('\n')
		if err == nil {
			line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			text := js.DecodeUTF8([]byte(line))
			if js.Trim(text) != "" {
				return js.Trim(text), nil
			}
			seen = append(seen, text)
			continue
		}
		if line != "" {
			seen = append(seen, js.DecodeUTF8([]byte(line)))
		}
		if errors.Is(err, io.EOF) {
			for _, s := range seen {
				if js.Trim(s) != "" {
					return js.Trim(s), nil
				}
			}
			return "", nil
		}
		return "", err
	}
}

// readAllStdin is all of stdin, for a text piped into "smstunnel send <number>".
func readAllStdin(in io.Reader) (string, error) {
	const max = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(in, max+1))
	if len(data) > max {
		return "", usageError("The text on standard input is too long.")
	}
	if err != nil {
		return "", err
	}
	return js.DecodeUTF8(data), nil
}

// toolRunner runs the program of a system key store: the exit status (-1 when it could not run or ran out of
// time), its output and its errors.
type toolRunner interface {
	run(name string, args []string, stdin string) (status int, stdout, stderr string)
}

type execTools struct{}

const toolTimeout = 20 * time.Second

func (execTools) run(name string, args []string, stdin string) (int, string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	hideWindow(cmd)
	status := 0
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && ctx.Err() == nil {
			status = exitErr.ExitCode()
		} else {
			status = -1
		}
		if status == 0 {
			status = -1
		}
	}
	return status, js.DecodeUTF8(stdout.Bytes()), js.DecodeUTF8(stderr.Bytes())
}
